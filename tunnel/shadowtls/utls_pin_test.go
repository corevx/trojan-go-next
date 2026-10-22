package shadowtls

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

// 防御性 pin（规格 §3/§6）：utls 升级若改变 ClientHello 布局（session_id
// 位置/长度）或 BuildHandshakeState 行为，本组测试立即报警，防止注入技法
// 静默失效导致签名字节错位。
func TestUTLSLayoutPin(t *testing.T) {
	uconn := utls.UClient(&deadConn{}, &utls.Config{ServerName: "www.example.com"}, utls.HelloChrome_Auto)
	if err := uconn.BuildHandshakeState(); err != nil {
		t.Fatal("BuildHandshakeState failed:", err)
	}
	hello := uconn.HandshakeState.Hello

	// chrome 类指纹必须自带 32 字节 legacy_session_id（TLS1.3 中间盒兼容），
	// 否则 v3 签名无处安放
	if len(hello.SessionId) != tlsSessionIDSize {
		t.Fatalf("utls HelloChrome_Auto session id length changed: %d (expected 32) — utls upgraded, re-validate the injection technique (spec §6)", len(hello.SessionId))
	}

	// 消息坐标偏移：session_id 长度字节在 Raw[38]，本体在 Raw[39:71]
	const msgSessionIDStart = 1 + 3 + 2 + tlsRandomSize + 1
	if len(hello.Raw) < msgSessionIDStart+tlsSessionIDSize {
		t.Fatalf("ClientHello too short: %d", len(hello.Raw))
	}
	if got := hello.Raw[msgSessionIDStart-1]; got != tlsSessionIDSize {
		t.Fatalf("session id length byte at Raw[%d] = %d, expected 32 — utls layout changed", msgSessionIDStart-1, got)
	}
	if !bytes.Equal(hello.Raw[msgSessionIDStart:msgSessionIDStart+tlsSessionIDSize], hello.SessionId) {
		t.Fatal("hello.Raw[39:71] no longer aliases hello.SessionId — utls layout changed, injection offset must be re-derived (spec §3)")
	}

	// 记录坐标偏移：加上 5 字节记录头后 session_id 在 frame[44:76]、
	// HMAC 槽在 frame[72:76]
	sid, err := signSessionIDMsg(hello.Raw, "pw")
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, 0, tlsHeaderSize+len(hello.Raw))
	record = append(record, recordHandshake, 3, 1)
	record = binary.BigEndian.AppendUint16(record, uint16(len(hello.Raw)))
	record = append(record, hello.Raw...)

	injected := append([]byte(nil), record...)
	copy(injected[tlsHeaderSize+msgSessionIDStart:], sid[:])

	u, err := verifyClientHello(injected, []user{{Name: "pin", Password: "pw"}})
	if err != nil {
		t.Fatal("production verify failed on injected utls ClientHello:", err)
	}
	if u.Name != "pin" {
		t.Fatal("wrong user matched")
	}
}

type deadConn struct{}

func (d *deadConn) Read(b []byte) (int, error)         { time.Sleep(time.Minute); return 0, io.EOF }
func (d *deadConn) Write(b []byte) (int, error)        { return 0, io.ErrClosedPipe }
func (d *deadConn) Close() error                       { return nil }
func (d *deadConn) LocalAddr() net.Addr                { return nil }
func (d *deadConn) RemoteAddr() net.Addr               { return nil }
func (d *deadConn) SetDeadline(t time.Time) error      { return nil }
func (d *deadConn) SetReadDeadline(t time.Time) error  { return nil }
func (d *deadConn) SetWriteDeadline(t time.Time) error { return nil }

// 端到端 pin：注入的签名 session_id 必须原样出现在真实握手的线上字节里。
// 服务端用 tee 连接捕获握手期读到的原始字节后解析，比 API 观察更严格——
// 看的就是线上字节（复刻 interop/scratch/utls-sessionid 证明程序，规格 §6）。
func TestUTLSInjectedSessionIDOnTheWire(t *testing.T) {
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var observed atomic.Value // []byte：服务侧读到的全部握手字节
	tee := func(c net.Conn) net.Conn { return &teeConn{Conn: c} }
	srvCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	handshakeDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			handshakeDone <- err
			return
		}
		defer conn.Close()
		tc := tee(conn).(*teeConn)
		tlsConn := tls.Server(tc, srvCfg)
		if err := tlsConn.Handshake(); err != nil {
			handshakeDone <- err
			return
		}
		observed.Store(append([]byte(nil), tc.read...))
		buf := make([]byte, 16)
		_, _ = tlsConn.Read(buf)
		handshakeDone <- nil
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	uconn := utls.UClient(client, &utls.Config{
		InsecureSkipVerify: true,
		ServerName:         "www.example.com",
		MinVersion:         tls.VersionTLS12,
	}, utls.HelloChrome_Auto)
	if err := uconn.BuildHandshakeState(); err != nil {
		t.Fatal(err)
	}
	hello := uconn.HandshakeState.Hello
	if len(hello.SessionId) != tlsSessionIDSize {
		t.Fatalf("session id length = %d, expected 32", len(hello.SessionId))
	}
	sid, err := signSessionIDMsg(hello.Raw, "pw")
	if err != nil {
		t.Fatal(err)
	}
	const msgSessionIDStart = 1 + 3 + 2 + tlsRandomSize + 1
	copy(hello.Raw[msgSessionIDStart:msgSessionIDStart+tlsSessionIDSize], sid[:])
	hello.SessionId = append([]byte(nil), sid[:]...)

	if err := uconn.HandshakeContext(context.Background()); err != nil {
		t.Fatal("utls handshake failed:", err)
	}
	if _, err := uconn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if err := <-handshakeDone; err != nil {
		t.Fatal("server-side handshake failed:", err)
	}

	raw, ok := observed.Load().([]byte)
	if !ok || len(raw) < tlsHeaderSize+msgSessionIDStart+tlsSessionIDSize {
		t.Fatal("server captured too few handshake bytes")
	}
	// 第一个记录必须是 ClientHello
	if raw[0] != recordHandshake || raw[tlsHeaderSize] != handshakeClientHello {
		t.Fatal("first wire record is not a ClientHello")
	}
	if !bytes.Equal(raw[tlsHeaderSize+msgSessionIDStart:tlsHeaderSize+msgSessionIDStart+tlsSessionIDSize], sid[:]) {
		t.Fatal("wire session id != injected — HandshakeContext re-serialized the ClientHello")
	}
}

type teeConn struct {
	net.Conn
	read []byte
}

func (t *teeConn) Read(p []byte) (int, error) {
	n, err := t.Conn.Read(p)
	if n > 0 {
		t.read = append(t.read, p[:n]...)
	}
	return n, err
}
