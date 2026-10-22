package tls

import (
	"context"
	stdtls "crypto/tls"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

// startSessionStack 起 transport+tls 服务端与指定配置的 tls 客户端，
// 并在后台持续消费 AcceptConn（读到 EOF 为止，保持服务端连接不被提前
// 关闭——RST 会丢弃客户端尚未处理的 NewSessionTicket）。
func startSessionStack(t *testing.T, reuse bool) (tunnel.Client, func()) {
	t.Helper()
	certPath := filepath.Join(t.TempDir(), "session-resume.crt")
	keyPath := filepath.Join(t.TempDir(), "session-resume.key")
	genRSATestCert(t, certPath, keyPath)

	serverCfg := &Config{
		TLS: TLSConfig{
			CertCheckRate:  1,
			ReuseSession:   true,                 // 服务端须允许发票据，客户端才有会话可复用
			ALPN:           []string{"http/1.1"}, // 显式协商，守护 SNI/ALPN 兼容性断言
			KeyPath:        keyPath,
			CertPath:       certPath,
			VerifyHostName: false,
		},
	}
	clientCfg := &Config{
		TLS: TLSConfig{
			Verify:       false,
			SNI:          "localhost",
			ReuseSession: reuse,
		},
	}

	port := common.PickPort("tcp", "127.0.0.1")
	ctx := config.WithConfig(context.Background(), transport.Name, &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	})
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)

	s, err := NewServer(config.WithConfig(context.Background(), Name, serverCfg), tcpServer)
	common.Must(err)
	c, err := NewClient(config.WithConfig(context.Background(), Name, clientCfg), tcpClient)
	common.Must(err)

	go func() {
		for {
			conn, err := s.AcceptConn(nil)
			if err != nil {
				return
			}
			go io.Copy(io.Discard, conn)
		}
	}()

	cleanup := func() {
		_ = c.Close()
		_ = s.Close()
	}
	return c, cleanup
}

// dialResumeHandshake 用被测客户端拨号并完成握手，返回该次握手是否为
// 会话复用及 ALPN 协商结果。TLS 1.3 的 NewSessionTicket 在握手后才送达，
// 给一次读窗口让客户端处理并缓存票据（与既有服务端侧测试同手法）。
func dialResumeHandshake(t *testing.T, c tunnel.Client) (resumed bool, alpn string) {
	t.Helper()
	conn, err := c.DialConn(nil, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := [1]byte{}
	_, _ = conn.Read(buf[:])

	raw := conn.(*transport.Conn).Conn
	switch tc := raw.(type) {
	case *stdtls.Conn:
		st := tc.ConnectionState()
		return st.DidResume, st.NegotiatedProtocol
	case *utls.UConn:
		st := tc.ConnectionState()
		return st.DidResume, st.NegotiatedProtocol
	default:
		t.Fatalf("unexpected underlying conn type %T", raw)
		return false, ""
	}
}

// TestClientSessionResumptionDefault stdlib 路径回归：reuse_session=true 时
// 第二条连接必须命中会话复用（DidResume），此前每次拨号新建 tls.Config 且
// 从不设置 ClientSessionCache，该配置项从未生效。
func TestClientSessionResumptionDefault(t *testing.T) {
	c, cleanup := startSessionStack(t, true)
	defer cleanup()

	if resumed, alpn := dialResumeHandshake(t, c); resumed || alpn != "http/1.1" {
		t.Fatalf("first conn should be full handshake with ALPN, got resumed=%v alpn=%q", resumed, alpn)
	}
	time.Sleep(200 * time.Millisecond) // 等服务端票据送达并缓存
	if resumed, alpn := dialResumeHandshake(t, c); !resumed || alpn != "http/1.1" {
		t.Fatalf("second conn failed to resume session (reuse_session=true), resumed=%v alpn=%q", resumed, alpn)
	}
}

// TestClientSessionNoReuseWhenDisabled reuse_session=false 保持"每次完整
// 握手"语义：即使服务端发票据，客户端无缓存也不得复用。
func TestClientSessionNoReuseWhenDisabled(t *testing.T) {
	c, cleanup := startSessionStack(t, false)
	defer cleanup()

	if resumed, _ := dialResumeHandshake(t, c); resumed {
		t.Fatal("first conn must not resume")
	}
	time.Sleep(200 * time.Millisecond)
	if resumed, _ := dialResumeHandshake(t, c); resumed {
		t.Fatal("second conn must not resume when reuse_session=false")
	}
}

// TestClientSessionResumptionFingerprint utls 指纹路径同样接上会话缓存。
// utls 指纹 hello 的形状由 spec 决定：spec 含 SessionTicketExtension 但不含
// PSK 扩展占位，上游刻意不支持 TLS 1.3 PSK 复用（会改变指纹形状），因此
// 指纹路径的会话复用发生在 TLS 1.2（会话票据缩写握手，与真实浏览器同款
// 行为）。服务端用裸 tls.Listener 限 TLS 1.2 验证。
func TestClientSessionResumptionFingerprint(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "fp-resume.crt")
	keyPath := filepath.Join(t.TempDir(), "fp-resume.key")
	genRSATestCert(t, certPath, keyPath)
	cert, err := stdtls.LoadX509KeyPair(certPath, keyPath)
	common.Must(err)

	ln, err := stdtls.Listen("tcp", "127.0.0.1:0", &stdtls.Config{
		Certificates: []stdtls.Certificate{cert},
		MaxVersion:   stdtls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	})
	common.Must(err)
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(io.Discard, conn)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	clientCfg := &Config{
		TLS: TLSConfig{
			Verify:       false,
			SNI:          "localhost",
			Fingerprint:  "chrome",
			ReuseSession: true,
		},
	}
	ctx := config.WithConfig(context.Background(), transport.Name, &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	})
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{}) // transport 客户端默认下垫是 freedom
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	c, err := NewClient(config.WithConfig(context.Background(), Name, clientCfg), tcpClient)
	common.Must(err)
	defer c.Close()

	if resumed, alpn := dialResumeHandshake(t, c); resumed || alpn != "http/1.1" {
		t.Fatalf("first conn should be full handshake with ALPN, got resumed=%v alpn=%q", resumed, alpn)
	}
	if resumed, alpn := dialResumeHandshake(t, c); !resumed || alpn != "http/1.1" {
		t.Fatalf("second conn failed to resume session via utls, resumed=%v alpn=%q", resumed, alpn)
	}
}
