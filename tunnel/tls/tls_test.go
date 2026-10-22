package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

func TestDefaultTLSRSA2048(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "server-rsa2048.crt")
	keyPath := filepath.Join(t.TempDir(), "server-rsa2048.key")
	genRSATestCert(t, certPath, keyPath)
	serverCfg := &Config{
		TLS: TLSConfig{
			VerifyHostName: true,
			CertCheckRate:  1,
			KeyPath:        keyPath,
			CertPath:       certPath,
		},
	}
	clientCfg := &Config{
		TLS: TLSConfig{
			Verify:      false,
			SNI:         "localhost",
			Fingerprint: "",
		},
	}
	sctx := config.WithConfig(context.Background(), Name, serverCfg)
	cctx := config.WithConfig(context.Background(), Name, clientCfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)
	common.Must(err)
	s, err := NewServer(sctx, tcpServer)
	common.Must(err)
	c, err := NewClient(cctx, tcpClient)
	common.Must(err)

	wg := sync.WaitGroup{}
	wg.Add(1)
	var conn1, conn2 net.Conn
	go func() {
		conn2, err = s.AcceptConn(nil)
		common.Must(err)
		wg.Done()
	}()
	conn1, err = c.DialConn(nil, nil)
	common.Must(err)

	common.Must2(conn1.Write([]byte("12345678\r\n")))
	wg.Wait()
	buf := [10]byte{}
	conn2.Read(buf[:])
	if !util.CheckConn(conn1, conn2) {
		t.Fail()
	}
	conn1.Close()
	conn2.Close()
}

func TestDefaultTLSECC(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "server-ecc.crt")
	keyPath := filepath.Join(t.TempDir(), "server-ecc.key")
	genPinTestCert(t, certPath, keyPath)
	serverCfg := &Config{
		TLS: TLSConfig{
			VerifyHostName: true,
			CertCheckRate:  1,
			KeyPath:        keyPath,
			CertPath:       certPath,
		},
	}
	clientCfg := &Config{
		TLS: TLSConfig{
			Verify:      false,
			SNI:         "localhost",
			Fingerprint: "",
		},
	}
	sctx := config.WithConfig(context.Background(), Name, serverCfg)
	cctx := config.WithConfig(context.Background(), Name, clientCfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)
	common.Must(err)
	s, err := NewServer(sctx, tcpServer)
	common.Must(err)
	c, err := NewClient(cctx, tcpClient)
	common.Must(err)

	wg := sync.WaitGroup{}
	wg.Add(1)
	var conn1, conn2 net.Conn
	go func() {
		conn2, err = s.AcceptConn(nil)
		common.Must(err)
		wg.Done()
	}()
	conn1, err = c.DialConn(nil, nil)
	common.Must(err)

	common.Must2(conn1.Write([]byte("12345678\r\n")))
	wg.Wait()
	buf := [10]byte{}
	conn2.Read(buf[:])
	if !util.CheckConn(conn1, conn2) {
		t.Fail()
	}
	conn1.Close()
	conn2.Close()
}

func TestUTLSRSA2048(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "server-rsa2048.crt")
	keyPath := filepath.Join(t.TempDir(), "server-rsa2048.key")
	genRSATestCert(t, certPath, keyPath)
	fingerprints := []string{
		"chrome",
		"firefox",
		"ios",
	}
	for _, s := range fingerprints {
		serverCfg := &Config{
			TLS: TLSConfig{
				CertCheckRate: 1,
				KeyPath:       keyPath,
				CertPath:      certPath,
			},
		}
		clientCfg := &Config{
			TLS: TLSConfig{
				Verify:      false,
				SNI:         "localhost",
				Fingerprint: s,
			},
		}
		sctx := config.WithConfig(context.Background(), Name, serverCfg)
		cctx := config.WithConfig(context.Background(), Name, clientCfg)

		port := common.PickPort("tcp", "127.0.0.1")
		transportConfig := &transport.Config{
			LocalHost:  "127.0.0.1",
			LocalPort:  port,
			RemoteHost: "127.0.0.1",
			RemotePort: port,
		}
		ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
		ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
		tcpClient, err := transport.NewClient(ctx, nil)
		common.Must(err)
		tcpServer, err := transport.NewServer(ctx, nil)
		common.Must(err)

		s, err := NewServer(sctx, tcpServer)
		common.Must(err)
		c, err := NewClient(cctx, tcpClient)
		common.Must(err)

		wg := sync.WaitGroup{}
		wg.Add(1)
		var conn1, conn2 net.Conn
		go func() {
			conn2, err = s.AcceptConn(nil)
			common.Must(err)
			wg.Done()
		}()
		conn1, err = c.DialConn(nil, nil)
		common.Must(err)

		common.Must2(conn1.Write([]byte("12345678\r\n")))
		wg.Wait()
		buf := [10]byte{}
		conn2.Read(buf[:])
		if !util.CheckConn(conn1, conn2) {
			t.Fail()
		}
		conn1.Close()
		conn2.Close()
		s.Close()
		c.Close()
	}
}

func TestUTLSECC(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "server-ecc.crt")
	keyPath := filepath.Join(t.TempDir(), "server-ecc.key")
	genPinTestCert(t, certPath, keyPath)
	fingerprints := []string{
		"chrome",
		"firefox",
		"ios",
	}
	for _, s := range fingerprints {
		serverCfg := &Config{
			TLS: TLSConfig{
				CertCheckRate: 1,
				KeyPath:       keyPath,
				CertPath:      certPath,
			},
		}
		clientCfg := &Config{
			TLS: TLSConfig{
				Verify:      false,
				SNI:         "localhost",
				Fingerprint: s,
			},
		}
		sctx := config.WithConfig(context.Background(), Name, serverCfg)
		cctx := config.WithConfig(context.Background(), Name, clientCfg)

		port := common.PickPort("tcp", "127.0.0.1")
		transportConfig := &transport.Config{
			LocalHost:  "127.0.0.1",
			LocalPort:  port,
			RemoteHost: "127.0.0.1",
			RemotePort: port,
		}
		ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
		ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
		tcpClient, err := transport.NewClient(ctx, nil)
		common.Must(err)
		tcpServer, err := transport.NewServer(ctx, nil)
		common.Must(err)

		s, err := NewServer(sctx, tcpServer)
		common.Must(err)
		c, err := NewClient(cctx, tcpClient)
		common.Must(err)

		wg := sync.WaitGroup{}
		wg.Add(1)
		var conn1, conn2 net.Conn
		go func() {
			conn2, err = s.AcceptConn(nil)
			common.Must(err)
			wg.Done()
		}()
		conn1, err = c.DialConn(nil, nil)
		common.Must(err)

		common.Must2(conn1.Write([]byte("12345678\r\n")))
		wg.Wait()
		buf := [10]byte{}
		conn2.Read(buf[:])
		if !util.CheckConn(conn1, conn2) {
			t.Fail()
		}
		conn1.Close()
		conn2.Close()
		s.Close()
		c.Close()
	}
}

func TestMatch(t *testing.T) {
	if !isDomainNameMatched("*.google.com", "www.google.com") {
		t.Fail()
	}

	if isDomainNameMatched("*.google.com", "google.com") {
		t.Fail()
	}

	if !isDomainNameMatched("localhost", "localhost") {
		t.Fail()
	}
}

// TestSilentConnClosedByDeadline 缺陷回归（P0）：建 TCP 连接后不发任何字节，
// 服务端 TLS 握手读此前无任何 deadline，静默连接会永久占用 per-conn goroutine。
// 修复后握手 + HTTP 请求读取阶段受 handshakeTimeout 限制：超时连接被服务端
// 关闭（客户端读到 EOF/RST 而非自己超时）；正常握手路径的 deadline 会被清除，
// 空闲超过 handshakeTimeout 的长连接仍可正常收发。
func TestSilentConnClosedByDeadline(t *testing.T) {
	origTimeout := handshakeTimeout
	handshakeTimeout = time.Second
	defer func() { handshakeTimeout = origTimeout }()

	certPath := filepath.Join(t.TempDir(), "silent-conn.crt")
	keyPath := filepath.Join(t.TempDir(), "silent-conn.key")
	genRSATestCert(t, certPath, keyPath)

	serverCfg := &Config{
		TLS: TLSConfig{
			VerifyHostName: true,
			KeyPath:        keyPath,
			CertPath:       certPath,
		},
	}
	clientCfg := &Config{
		TLS: TLSConfig{
			Verify: false,
			SNI:    "localhost",
		},
	}
	sctx := config.WithConfig(context.Background(), Name, serverCfg)
	cctx := config.WithConfig(context.Background(), Name, clientCfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx := config.WithConfig(context.Background(), transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)
	// 注意顺序：handshakeTimeout 已在 NewServer 之前改写（goroutine 创建
	// 提供 happens-before），defer 恢复放在 Close 之后执行
	s, err := NewServer(sctx, tcpServer)
	common.Must(err)
	c, err := NewClient(cctx, tcpClient)
	common.Must(err)
	defer c.Close()
	defer s.Close()

	// 1) 静默连接：不发任何字节。客户端读超时给 5s（> 服务端 1s deadline），
	//    若服务端按期关闭则读到 EOF/RST；若客户端超时先触发说明修复回归。
	silent, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	common.Must(err)
	defer silent.Close()
	silent.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := [1]byte{}
	_, rerr := silent.Read(buf[:])
	if rerr == nil {
		t.Fatal("silent conn read unexpectedly got data")
	}
	if ne, ok := rerr.(net.Error); ok && ne.Timeout() {
		t.Fatalf("silent conn not closed by server within 5s, client read timed out first: %v", rerr)
	}

	// 2) 正常路径不受影响：握手→首包→空闲超过 handshakeTimeout→仍可收发
	var conn2 tunnel.Conn
	accepted := make(chan struct{})
	go func() {
		var aerr error
		conn2, aerr = s.AcceptConn(nil)
		if aerr != nil {
			panic(aerr)
		}
		close(accepted)
	}()
	conn1, err := c.DialConn(nil, nil)
	common.Must(err)
	common.Must2(conn1.Write([]byte("12345678\r\n")))
	<-accepted
	// 消费 rewind 重放的首包（与既有测试一致），后续 CheckConn 才是干净流
	rbuf := [10]byte{}
	conn2.Read(rbuf[:])
	time.Sleep(1500 * time.Millisecond) // 空闲超过 handshakeTimeout(1s)
	if !util.CheckConn(conn1, conn2) {
		t.Fatal("normal conn unusable after idle > handshakeTimeout, deadline not cleared")
	}
	conn1.Close()
	conn2.Close()
}

// certSHA256Hex 计算一张 PEM 证书的 SHA-256 指纹（hex 小写）。
func certSHA256Hex(t *testing.T, pemStr string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("bad test pem")
	}
	crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(crt.Raw)
	return hex.EncodeToString(sum[:])
}

// genRSATestCert 运行时生成一张自签 RSA 2048 证书落盘（原内置测试证书
// 2026-09 过期，服务端侧证书加载有有效期检查，加载即拒）。
func genRSATestCert(t *testing.T, certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, keyOut, 0o600); err != nil {
		t.Fatal(err)
	}
}

// genPinTestCert 运行时生成一张自签 ECDSA 证书落盘，返回其 SHA-256 指纹。
// （既有内置测试证书已过期，服务端侧证书加载有有效期检查。）
func genPinTestCert(t *testing.T, certPath, keyPath string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certOut, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyOut, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// TestCertPinSHA256 证书指纹钉死：正确 pin（裸 hex 与冒号分组两种写法）
// 握手通过；错误 pin 握手被拒。覆盖 stdlib 与 utls 两条客户端路径。
// 证书运行时自签（既有内置测试证书 2026-09 已过期，服务端加载即拒）。
func TestCertPinSHA256(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "pin-test.crt")
	keyPath := filepath.Join(t.TempDir(), "pin-test.key")
	good := genPinTestCert(t, certPath, keyPath)
	grouped := ""
	for i := 0; i < len(good); i += 2 {
		if i > 0 {
			grouped += ":"
		}
		grouped += good[i : i+2]
	}
	bad := strings.Repeat("0", 64)

	startPair := func(t *testing.T, pin, fingerprint string) (tunnel.Client, tunnel.Server) {
		t.Helper()
		serverCfg := &Config{
			TLS: TLSConfig{
				CertCheckRate: 1,
				KeyPath:       keyPath,
				CertPath:      certPath,
			},
		}
		clientCfg := &Config{
			TLS: TLSConfig{
				Verify:        false,
				SNI:           "localhost",
				Fingerprint:   fingerprint,
				CertPinSHA256: pin,
			},
		}
		port := common.PickPort("tcp", "127.0.0.1")
		ctx := config.WithConfig(context.Background(), transport.Name, &transport.Config{
			LocalHost: "127.0.0.1", LocalPort: port,
			RemoteHost: "127.0.0.1", RemotePort: port,
		})
		ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
		tcpClient, err := transport.NewClient(ctx, nil)
		common.Must(err)
		tcpServer, err := transport.NewServer(ctx, nil)
		common.Must(err)
		s, err := NewServer(config.WithConfig(context.Background(), Name, serverCfg), tcpServer)
		common.Must(err)
		c, err := NewClient(config.WithConfig(context.Background(), Name, clientCfg), tcpClient)
		common.Must(err)
		return c, s
	}

	cases := []struct {
		name        string
		pin         string
		fingerprint string
		wantOK      bool
	}{
		{"good-hex", good, "", true},
		{"good-grouped", grouped, "chrome", true},
		{"bad-pin", bad, "", false},
		{"bad-pin-utls", bad, "chrome", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, s := startPair(t, tc.pin, tc.fingerprint)
			defer c.Close()
			defer s.Close()
			go func() { _, _ = s.AcceptConn(nil) }()
			conn, err := c.DialConn(nil, nil)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("dial with correct pin should succeed: %v", err)
				}
				conn.Close()
				return
			}
			if err == nil {
				conn.Close()
				t.Fatal("dial with wrong pin must fail")
			}
		})
	}
}
