package shadowtls

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"math/big"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/tunnel"
	utls "github.com/refraction-networking/utls"
)

// ---------------------------------------------------------------------------
// 测试基建
// ---------------------------------------------------------------------------

// startRealTLSSite 启动真实 TLS 站点（自签 CN=www.example.com，HTTP 应答），
// 模拟被伪装的真站。maxVersion 控制协商版本（1.3 / 1.2）。
func startRealTLSSite(t *testing.T, maxVersion uint16) (addr string) {
	t.Helper()
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   maxVersion,
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				tlsConn := tls.Server(conn, cfg)
				if err := tlsConn.Handshake(); err != nil {
					return
				}
				buf := make([]byte, 4096)
				_ = tlsConn.SetReadDeadline(time.Now().Add(10 * time.Second))
				_, _ = tlsConn.Read(buf)
				_, _ = tlsConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 7\r\n\r\nrealser"))
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "www.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"www.example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// newTestServer 构造最小 Server（绕过配置系统），直接驱动 handleConn
func newTestServer(t *testing.T, strict bool, users []user, dest string) *Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Server{
		users:    users,
		dest:     dest,
		strict:   strict,
		wildcard: wildcardSNIOff,
		dialer:   &net.Dialer{Timeout: destDialTimeout},
		connChan: make(chan tunnel.Conn, 4),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func runHandleConn(t *testing.T, s *Server) (client net.Conn) {
	t.Helper()
	client, serverEnd := net.Pipe()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic in handleConn: %v", r)
			}
		}()
		s.handleConn(serverEnd)
	}()
	return client
}

// readRawFrame 从裸连接读一个完整 TLS 记录（脚本化测试用）
func readRawFrame(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	header := make([]byte, tlsHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatal("read frame header:", err)
	}
	length := int(binary.BigEndian.Uint16(header[3:]))
	frame := make([]byte, tlsHeaderSize+length)
	copy(frame, header)
	if _, err := io.ReadFull(conn, frame[tlsHeaderSize:]); err != nil {
		t.Fatal("read frame body:", err)
	}
	return frame
}

// rawDest 启动一个原样应答的 TCP 目标，用于回退/中继路径断言
func rawDest(t *testing.T) (addr string, received chan []byte, send func([]byte)) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	received = make(chan []byte, 32)
	holder := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		holder <- conn
		buf := make([]byte, 16384)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				b := append([]byte(nil), buf[:n]...)
				select {
				case received <- b:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	send = func(b []byte) {
		select {
		case c := <-holder:
			_, _ = c.Write(b)
			holder <- c
		case <-time.After(2 * time.Second):
			t.Error("rawDest send timeout")
		}
	}
	return ln.Addr().String(), received, send
}

// ---------------------------------------------------------------------------
// 脚本化 v3 客户端（镜像 mihomo client 语义：握手包装层 + 数据期 verified 连接）
// ---------------------------------------------------------------------------

// v3ClientStream 握手期包装：首个 ServerHello 单次播种（HRR 对齐，§4.8），
// appdata 帧验链R tag → XOR 解码 → 剥 tag → 还原记录头
type v3ClientStream struct {
	net.Conn
	password     string
	serverRandom []byte
	readHMAC     hash.Hash // 链R
	readHMACKey  []byte
	isTLS13      bool
	authorized   bool
	pending      []byte
}

func (c *v3ClientStream) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	frame, err := readFrame(c.Conn)
	if err != nil {
		return 0, err
	}
	switch frame[0] {
	case recordHandshake:
		if len(frame) >= serverRandomIndex+tlsRandomSize && frame[tlsHeaderSize] == handshakeServerHello && c.readHMAC == nil {
			c.serverRandom = append([]byte(nil), frame[serverRandomIndex:serverRandomIndex+tlsRandomSize]...)
			c.readHMAC = hmac.New(sha1.New, []byte(c.password))
			_, _ = c.readHMAC.Write(c.serverRandom)
			c.readHMACKey = kdf(c.password, c.serverRandom)
			c.isTLS13 = isServerHelloTLS13(frame)
			// 非 TLS1.3 真站：握手期无改写帧，见 ServerHello 即视为对端可信
			c.authorized = !c.isTLS13
		}
	case recordApplicationData:
		c.authorized = false
		if len(frame) > tlsHMACHeaderSize && c.readHMAC != nil {
			_, _ = c.readHMAC.Write(frame[tlsHMACHeaderSize:])
			if !hmac.Equal(c.readHMAC.Sum(nil)[:hmacSize], frame[tlsHeaderSize:tlsHMACHeaderSize]) {
				return 0, errors.New("v3 test client: handshake frame HMAC mismatch")
			}
			xorSlice(frame[tlsHMACHeaderSize:], c.readHMACKey)
			copy(frame[hmacSize:hmacSize+tlsHeaderSize], frame[:tlsHeaderSize])
			frame = frame[hmacSize:]
			binary.BigEndian.PutUint16(frame[3:5], uint16(len(frame)-tlsHeaderSize))
			c.authorized = true
		}
	}
	c.pending = frame
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// clientVerifiedConn 数据期客户端连接：写=链C（tag 回喂），读=链S，
// hmacIgnore=链R 副本（过滤真站残留票帧）
type clientVerifiedConn struct {
	net.Conn
	writeMu    sync.Mutex
	hmacAdd    hash.Hash
	hmacVerify hash.Hash
	hmacIgnore hash.Hash
	pending    []byte
}

func (c *clientVerifiedConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	for {
		frame, err := readFrame(c.Conn)
		if err != nil {
			return 0, err
		}
		switch frame[0] {
		case recordAlert:
			return 0, errors.New("v3 test client: remote alert")
		case recordApplicationData:
			if c.hmacIgnore != nil {
				if verifyApplicationData(frame, c.hmacIgnore, false) {
					continue
				}
				c.hmacIgnore = nil
			}
			if !verifyApplicationData(frame, c.hmacVerify, true) {
				return 0, errors.New("v3 test client: application data verification failed")
			}
			c.pending = frame[tlsHMACHeaderSize:]
			n := copy(p, c.pending)
			c.pending = c.pending[n:]
			return n, nil
		default:
			return 0, errors.New("v3 test client: unexpected record type")
		}
	}
}

func (c *clientVerifiedConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	total := len(p)
	for len(p) > 0 {
		length := len(p)
		if length > maxTLSPlaintext {
			length = maxTLSPlaintext
		}
		var header [tlsHMACHeaderSize]byte
		header[0], header[1], header[2] = recordApplicationData, 3, 3
		binary.BigEndian.PutUint16(header[3:], uint16(hmacSize+length))
		_, _ = c.hmacAdd.Write(p[:length])
		tag := c.hmacAdd.Sum(nil)[:hmacSize]
		_, _ = c.hmacAdd.Write(tag)
		copy(header[tlsHeaderSize:], tag)
		if err := writeBuffers(c.Conn, header[:], p[:length]); err != nil {
			return total - len(p), err
		}
		p = p[length:]
	}
	return total, nil
}

// signSessionIDMsg 按规格 §4.1 对 ClientHello 握手消息（含 4 字节握手头，
// 即 utls hello.Raw）签名，返回 28 随机 + 4 HMAC 的 session_id
func signSessionIDMsg(raw []byte, password string) ([tlsSessionIDSize]byte, error) {
	var sid [tlsSessionIDSize]byte
	const msgSessionIDStart = 1 + 3 + 2 + tlsRandomSize + 1
	if len(raw) < msgSessionIDStart+tlsSessionIDSize {
		return sid, errors.New("client hello too short")
	}
	if _, err := rand.Read(sid[:tlsSessionIDSize-hmacSize]); err != nil {
		return sid, err
	}
	h := hmac.New(sha1.New, []byte(password))
	_, _ = h.Write(raw[:msgSessionIDStart])
	_, _ = h.Write(sid[:]) // 末 4 字节为零参与运算
	_, _ = h.Write(raw[msgSessionIDStart+tlsSessionIDSize:])
	copy(sid[tlsSessionIDSize-hmacSize:], h.Sum(nil)[:hmacSize])
	return sid, nil
}

// utlsV3Handshake 用 utls chrome 指纹跑完整 v3 握手（session_id 注入技法
// 见规格 §6），返回数据期客户端连接
func utlsV3Handshake(t *testing.T, conn net.Conn, password string) (*clientVerifiedConn, error) {
	t.Helper()
	stream := &v3ClientStream{Conn: conn, password: password}
	tlsCfg := &utls.Config{
		InsecureSkipVerify: true,
		ServerName:         "www.example.com",
		MinVersion:         tls.VersionTLS12,
	}
	uconn := utls.UClient(stream, tlsCfg, utls.HelloChrome_Auto)
	if err := uconn.BuildHandshakeState(); err != nil {
		return nil, err
	}
	hello := uconn.HandshakeState.Hello
	if len(hello.SessionId) != tlsSessionIDSize {
		return nil, common.NewError("utls fingerprint no longer carries a 32-byte session id (layout changed?)")
	}
	sid, err := signSessionIDMsg(hello.Raw, password)
	if err != nil {
		return nil, err
	}
	const msgSessionIDStart = 1 + 3 + 2 + tlsRandomSize + 1
	copy(hello.Raw[msgSessionIDStart:msgSessionIDStart+tlsSessionIDSize], sid[:])
	hello.SessionId = append([]byte(nil), sid[:]...)
	if err := uconn.HandshakeContext(context.Background()); err != nil {
		return nil, err
	}
	if !stream.authorized {
		return nil, errors.New("v3 test client: server not authorized (no valid rewritten frame seen)")
	}

	c := &clientVerifiedConn{Conn: conn}
	c.hmacAdd = hmac.New(sha1.New, []byte(password))
	_, _ = c.hmacAdd.Write(stream.serverRandom)
	_, _ = c.hmacAdd.Write([]byte("C"))
	c.hmacVerify = hmac.New(sha1.New, []byte(password))
	_, _ = c.hmacVerify.Write(stream.serverRandom)
	_, _ = c.hmacVerify.Write([]byte("S"))
	c.hmacIgnore = stream.readHMAC
	return c, nil
}

// ---------------------------------------------------------------------------
// 全流程测试：真实 TLS 栈（utls 客户端 × Go 真站）
// ---------------------------------------------------------------------------

// 矩阵用例 5（strict × TLS1.3 真站）单元级等价：utls 客户端 ↔ 本服务端 ↔
// Go TLS1.3 真站，验签 → 握手中继 → 劫持 → 数据期双向往返
func TestServeV3HappyPathRealStacks(t *testing.T) {
	dest := startRealTLSSite(t, tls.VersionTLS13)
	server := newTestServer(t, true, []user{{Name: "u1", Password: "pw"}}, dest)
	client := runHandleConn(t, server)

	cvc, err := utlsV3Handshake(t, client, "pw")
	if err != nil {
		t.Fatal("handshake failed:", err)
	}

	// 客户端收流需与写流并发：服务端握手中继的真站方向 goroutine 可能还
	// 有改写帧在写向客户端（net.Pipe 无缓冲，不并发读会卡住对端）
	clientRecv := make(chan []byte, 64)
	go func() {
		for {
			buf := make([]byte, 32768)
			n, err := cvc.Read(buf)
			if n > 0 {
				clientRecv <- buf[:n]
			}
			if err != nil {
				close(clientRecv)
				return
			}
		}
	}()
	collect := func(want int) []byte {
		var out []byte
		deadline := time.After(5 * time.Second)
		for len(out) < want {
			select {
			case b, ok := <-clientRecv:
				if !ok {
					t.Fatalf("client recv closed, got %d/%d bytes", len(out), want)
				}
				out = append(out, b...)
			case <-deadline:
				t.Fatalf("client recv timeout, got %d/%d bytes", len(out), want)
			}
		}
		return out
	}

	// 首个数据写 = auth 帧，payload 为内层请求
	request := []byte("TROJAN-REQUEST-HELLO")
	if _, err := cvc.Write(request); err != nil {
		t.Fatal(err)
	}

	select {
	case inner := <-server.connChan:
		got := make([]byte, len(request))
		if _, err := io.ReadFull(inner, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, request) {
			t.Fatalf("auth payload mismatch: %q", got)
		}
		// 内层 → 客户端方向（链S），双份大 payload 触发写侧分片
		response := bytes.Repeat([]byte("R"), 40000)
		go func() {
			_, _ = inner.Write(response)
			_, _ = inner.Write(response)
		}()
		if got := collect(len(response) * 2); !bytes.Equal(got, append(append([]byte{}, response...), response...)) {
			t.Fatal("server->client payload mismatch")
		}
		// 客户端 → 内层方向（链C）再来一轮，验证链状态跨帧推进。
		// net.Pipe 无缓冲：写必须与读并发，否则自死锁
		second := []byte("SECOND-ROUND")
		writeDone := make(chan error, 1)
		go func() {
			_, err := cvc.Write(second)
			writeDone <- err
		}()
		got2 := make([]byte, len(second))
		if _, err := io.ReadFull(inner, got2); err != nil {
			t.Fatal(err)
		}
		if err := <-writeDone; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got2, second) {
			t.Fatal("second round client->server mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not deliver authenticated conn")
	}
}

// 矩阵用例 7 单元级等价：非 strict × TLS1.2 真站仍可用（链R 闲置，授权
// 走「见过 ServerHello 即真」分支）
func TestServeV3LossyTLS12RealStack(t *testing.T) {
	dest := startRealTLSSite(t, tls.VersionTLS12)
	server := newTestServer(t, false, []user{{Name: "u1", Password: "pw"}}, dest)
	client := runHandleConn(t, server)

	cvc, err := utlsV3Handshake(t, client, "pw")
	if err != nil {
		t.Fatal("handshake failed:", err)
	}
	if _, err := cvc.Write([]byte("REQ12")); err != nil {
		t.Fatal(err)
	}
	select {
	case inner := <-server.connChan:
		got := make([]byte, 5)
		if _, err := io.ReadFull(inner, got); err != nil {
			t.Fatal(err)
		}
		if string(got) != "REQ12" {
			t.Fatalf("payload mismatch: %q", got)
		}
		// 无缓冲管道：写与读并发
		writeDone := make(chan error, 1)
		go func() {
			_, err := inner.Write([]byte("ACK12"))
			writeDone <- err
		}()
		buf := make([]byte, 5)
		if _, err := io.ReadFull(cvc, buf); err != nil {
			t.Fatal(err)
		}
		if err := <-writeDone; err != nil {
			t.Fatal(err)
		}
		if string(buf) != "ACK12" {
			t.Fatal("response mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lossy TLS1.2 flow did not deliver conn")
	}
}

// ---------------------------------------------------------------------------
// 脚本化流程测试（无 TLS 栈，字节级确定性）
// ---------------------------------------------------------------------------

// 矩阵用例 3/4 单元级等价：无有效签名的探测连接 → 纯中继到真站，字节
// 原样透传，服务端不回 alert、不阻断
func TestServeV3FallbackProbe(t *testing.T) {
	destAddr, received, send := rawDest(t)
	server := newTestServer(t, true, []user{{Name: "u1", Password: "pw"}}, destAddr)
	client := runHandleConn(t, server)

	// 无签名 ClientHello（占位 sid）+ 后续任意字节
	ch := craftClientHello("www.example.com")
	if _, err := client.Write(ch); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("PROBE-FOLLOW-UP")); err != nil {
		t.Fatal(err)
	}

	// 真站应原样收到 CH 与后续字节
	got1 := <-received
	if !bytes.HasPrefix(got1, ch) {
		t.Fatal("dest did not receive verbatim ClientHello")
	}
	// 客户端应收到真站回字节（经服务端中继）
	go send([]byte("REAL-SITE-RESPONSE"))
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil || string(buf[:n]) != "REAL-SITE-RESPONSE" {
		t.Fatalf("probe should receive real-site bytes, got %q err %v", buf[:n], err)
	}
	// 不应进入数据期
	select {
	case <-server.connChan:
		t.Fatal("unauthenticated conn must not reach the inner layer")
	case <-time.After(300 * time.Millisecond):
	}
}

// 矩阵用例 6 单元级等价：strict × 非 TLS1.3 真站 → 整条连接纯双向中继
func TestServeV3StrictDegradeToRelay(t *testing.T) {
	destAddr, received, send := rawDest(t)
	server := newTestServer(t, true, []user{{Name: "u1", Password: "pw"}}, destAddr)
	client := runHandleConn(t, server)

	ch := craftClientHello("www.example.com")
	testSignClientHello(t, ch, "pw") // 合法签名也要走 strict 降级路径
	if _, err := client.Write(ch); err != nil {
		t.Fatal(err)
	}

	// CH 到达脚本真站后回 TLS1.2 形态 ServerHello
	<-received
	send(craftServerHello(false))
	// 客户端应原样收到该 ServerHello
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if got := readRawFrame(t, client); !bytes.Equal(got, craftServerHello(false)) {
		t.Fatal("ServerHello must be forwarded verbatim in degrade path")
	}

	// 后续客户端字节应原样到真站，真站字节原样回客户端（无 HMAC 无改写）
	if _, err := client.Write([]byte("PLAIN-BYTES")); err != nil {
		t.Fatal(err)
	}
	relayed := <-received
	if !bytes.Contains(relayed, []byte("PLAIN-BYTES")) {
		t.Fatal("post-degrade client bytes must be relayed verbatim")
	}
	go send([]byte("DEST-BYTES"))
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil || !bytes.Contains(buf[:n], []byte("DEST-BYTES")) {
		t.Fatalf("client should receive dest bytes verbatim, got %q err %v", buf[:n], err)
	}
	select {
	case <-server.connChan:
		t.Fatal("strict-degraded conn must not reach the inner layer")
	case <-time.After(300 * time.Millisecond):
	}
}

// 规格§4.8 HRR 绑定：链种子必须取第一个 ServerHello（HRR）的 random。
// 脚本化字节流验证：HRR 首帧播种 → 改写帧与 auth 帧全部与 HRR random 对齐。
func TestServeV3HRRBindsFirstServerHello(t *testing.T) {
	// HRR ServerHello：RFC8446 §4.1.4 固定 random + supported_versions=0x0304
	hrrRandom := []byte{
		0xCF, 0x21, 0xAD, 0x74, 0xE5, 0x9A, 0x61, 0x11,
		0xBE, 0x1D, 0x8C, 0x02, 0x1E, 0x65, 0xB8, 0x91,
		0xC2, 0xA2, 0x11, 0x16, 0x7A, 0xBB, 0x8C, 0x5E,
		0x07, 0x9E, 0x09, 0xE2, 0xC8, 0xA8, 0x33, 0x9C,
	}
	body := make([]byte, 0, 96)
	body = binary.BigEndian.AppendUint16(body, 0x0303)
	body = append(body, hrrRandom...)
	body = append(body, tlsSessionIDSize)
	for i := 0; i < tlsSessionIDSize; i++ {
		body = append(body, byte(0x42))
	}
	body = binary.BigEndian.AppendUint16(body, 0x1301)
	body = append(body, 0)
	body = binary.BigEndian.AppendUint16(body, 2+2+2)
	body = binary.BigEndian.AppendUint16(body, 43)
	body = binary.BigEndian.AppendUint16(body, 2)
	body = binary.BigEndian.AppendUint16(body, 0x0304)
	hrrFrame := make([]byte, 0, tlsHeaderSize+4+len(body))
	hrrFrame = append(hrrFrame, recordHandshake, 3, 3)
	hrrFrame = binary.BigEndian.AppendUint16(hrrFrame, uint16(4+len(body)))
	hrrFrame = append(hrrFrame, handshakeServerHello)
	hrrFrame = append(hrrFrame, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	hrrFrame = append(hrrFrame, body...)

	destAddr, received, send := rawDest(t)
	server := newTestServer(t, true, []user{{Name: "u1", Password: "pw"}}, destAddr)
	client := runHandleConn(t, server)

	ch := craftClientHello("www.example.com")
	testSignClientHello(t, ch, "pw")
	if _, err := client.Write(ch); err != nil {
		t.Fatal(err)
	}
	<-received
	send(hrrFrame)
	// 客户端原样收到 HRR
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if got := readRawFrame(t, client); !bytes.Equal(got, hrrFrame) {
		t.Fatal("HRR must be forwarded verbatim")
	}

	// 客户端发第二 ClientHello（真实 HRR 流程），应原样转发真站
	ch2 := craftClientHello("www.example.com")
	if _, err := client.Write(ch2); err != nil {
		t.Fatal(err)
	}
	relayed := <-received
	if !bytes.HasPrefix(relayed, ch2) {
		t.Fatal("second ClientHello must be relayed verbatim")
	}

	// 真站发 appdata 帧（模拟加密握手段）：服务端须以 HRR random 播种的
	// 链R 改写（XOR + tag 插入）
	password := "pw"
	writeKey := kdf(password, hrrRandom)
	origPayload := []byte("ENCRYPTED-HANDSHAKE")
	appdata := append([]byte{recordApplicationData, 3, 3, 0, byte(len(origPayload))}, origPayload...)
	send(appdata)

	rewritten := readRawFrame(t, client)
	if rewritten[0] != recordApplicationData {
		t.Fatal("expected rewritten appdata record")
	}
	if got := int(binary.BigEndian.Uint16(rewritten[3:5])); got != hmacSize+len(origPayload) {
		t.Fatalf("rewritten frame length wrong: %d", got)
	}
	tag, payload := rewritten[tlsHeaderSize:tlsHMACHeaderSize], rewritten[tlsHMACHeaderSize:]
	// 链R 喂的是 XOR 后（线上形态）的数据——先验 tag 再解码（规格 §4.5）
	hR := hmac.New(sha1.New, []byte(password))
	_, _ = hR.Write(hrrRandom)
	_, _ = hR.Write(payload)
	if !hmac.Equal(tag, hR.Sum(nil)[:hmacSize]) {
		t.Fatal("rewritten frame tag does not bind the FIRST (HRR) server random")
	}
	xorSlice(payload, writeKey)
	if !bytes.Equal(payload, origPayload) {
		t.Fatal("XOR decode mismatch")
	}

	// auth 帧同样以 HRR random 播种的链C 计算 → 服务端必须命中
	hC := hmac.New(sha1.New, []byte(password))
	_, _ = hC.Write(hrrRandom)
	_, _ = hC.Write([]byte("C"))
	authPayload := []byte("HRR-AUTH-PAYLOAD")
	_, _ = hC.Write(authPayload)
	authTag := append([]byte(nil), hC.Sum(nil)[:hmacSize]...)
	frame := make([]byte, tlsHMACHeaderSize+len(authPayload))
	frame[0], frame[1], frame[2] = recordApplicationData, 3, 3
	binary.BigEndian.PutUint16(frame[3:], uint16(hmacSize+len(authPayload)))
	copy(frame[tlsHeaderSize:], authTag)
	copy(frame[tlsHMACHeaderSize:], authPayload)
	if _, err := client.Write(frame); err != nil {
		t.Fatal(err)
	}

	select {
	case inner := <-server.connChan:
		got := make([]byte, len(authPayload))
		if _, err := io.ReadFull(inner, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, authPayload) {
			t.Fatalf("HRR auth payload mismatch: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("auth frame bound to HRR random was not accepted")
	}
}

// ---------------------------------------------------------------------------
// 配置校验
// ---------------------------------------------------------------------------

type fakeUnderlay struct{}

func (f *fakeUnderlay) AcceptConn(tunnel.Tunnel) (tunnel.Conn, error) { return nil, io.EOF }
func (f *fakeUnderlay) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	return nil, io.EOF
}
func (f *fakeUnderlay) Close() error { return nil }

func TestNewServerConfigValidation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	goodHandshake := HandshakeConfig{Server: host, ServerPort: port}

	mkCtx := func(cfg *Config) context.Context {
		return config.WithConfig(context.Background(), Name, cfg)
	}

	// version 2 → 明确拒绝
	_, err = NewServer(mkCtx(&Config{ShadowTLS: ShadowTLSConfig{
		Enabled: true, Version: 2, Users: []UserConfig{{Name: "u", Password: "p"}}, Handshake: goodHandshake, Strict: true,
	}}), nil)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("only supports v3")) {
		t.Fatalf("expected v3-only rejection, got %v", err)
	}

	// 空 users → 拒绝
	if _, err = NewServer(mkCtx(&Config{ShadowTLS: ShadowTLSConfig{
		Enabled: true, Version: 3, Handshake: goodHandshake, Strict: true,
	}}), nil); err == nil {
		t.Fatal("expected rejection for empty users")
	}

	// 缺真站 → 拒绝
	if _, err = NewServer(mkCtx(&Config{ShadowTLS: ShadowTLSConfig{
		Enabled: true, Version: 3, Users: []UserConfig{{Name: "u", Password: "p"}}, Strict: true,
	}}), nil); err == nil {
		t.Fatal("expected rejection for missing handshake dest")
	}

	// 非法 wildcard → 拒绝
	if _, err = NewServer(mkCtx(&Config{ShadowTLS: ShadowTLSConfig{
		Enabled: true, Version: 3, Users: []UserConfig{{Name: "u", Password: "p"}}, Handshake: goodHandshake,
		Strict: true, WildcardSNI: "bogus",
	}}), nil); err == nil {
		t.Fatal("expected rejection for invalid wildcard-sni")
	}

	// version 缺省 → 默认 3 且可创建
	srv, err := NewServer(mkCtx(&Config{ShadowTLS: ShadowTLSConfig{
		Enabled: true, Users: []UserConfig{{Name: "u", Password: "p"}}, Handshake: goodHandshake, Strict: true,
	}}), &fakeUnderlay{})
	if err != nil {
		t.Fatal("expected success with default version, got", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
}
