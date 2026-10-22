package tls

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
	"github.com/p4gefau1t/trojan-go/tunnel/websocket"
)

// sniffTestFixture 搭建一对 tls server/client（复用 genRSATestCert 生成
// 运行时自签证书），供首包嗅探分流回归测试使用。
type sniffTestFixture struct {
	port   int
	server *Server
	client *Client
}

func newSniffTestFixture(t *testing.T) *sniffTestFixture {
	t.Helper()
	certPath := fmt.Sprintf("%s/sniff.crt", t.TempDir())
	keyPath := fmt.Sprintf("%s/sniff.key", t.TempDir())
	genRSATestCert(t, certPath, keyPath)

	serverCfg := &Config{TLS: TLSConfig{KeyPath: keyPath, CertPath: certPath}}
	clientCfg := &Config{TLS: TLSConfig{Verify: false, SNI: "localhost"}}
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
	f := &sniffTestFixture{port: port, server: s, client: c}
	t.Cleanup(func() {
		c.Close()
		s.Close()
		// redirector worker 退出时会打 log.Debug，而 log 包全局 logger
		// 无锁：等它打完再放行后续测试，避免与 TestCheckKeyPairLoop*
		// 的 RegisterLogger 交错触发 DATA RACE（log 包存量隐患）
		time.Sleep(50 * time.Millisecond)
	})
	return f
}

// acceptAsync 起后台 goroutine 调 AcceptConn，结果经 channel 返回。
func (f *sniffTestFixture) acceptAsync(overlay tunnel.Tunnel) <-chan tunnel.Conn {
	ch := make(chan tunnel.Conn, 1)
	go func() {
		conn, err := f.server.AcceptConn(overlay)
		if err != nil {
			close(ch)
			return
		}
		ch <- conn
	}()
	return ch
}

// readFull 循环 Read 至填满或出错（RewindConn 回放期每次 Read 只交付
// 缓冲内剩余字节，须逐次累积）。
func readFull(c io.Reader, buf []byte) error {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return err
		}
	}
	return nil
}

// TestSniffNonHTTPGoesToTrojanPath 首包不是合法 HTTP 请求 → 必须走 trojan
// 直连路径（connChan，AcceptConn(nil)），且首包字节经 rewind 回放完整
// 交付、后续流双向可用。RewindConn 缓冲释放优化（#6）的分流生命线回归。
func TestSniffNonHTTPGoesToTrojanPath(t *testing.T) {
	f := newSniffTestFixture(t)
	accepted := f.acceptAsync(nil)

	conn1, err := f.client.DialConn(nil, nil)
	common.Must(err)
	defer conn1.Close()
	// 非 HTTP 首包：trojan 头形态（hex hash + CRLF + 地址结构）
	firstPacket := []byte("38f1c2c39fbb4411553a86e3ac6d78e88a6fb2d4759c2e0f7a1b3c4d5e6f7081\r\n\x01\x03\x0blocalhost\x00\x50")
	common.Must2(conn1.Write(firstPacket))

	var conn2 tunnel.Conn
	select {
	case conn2 = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("trojan-path accept timed out")
	}
	if conn2 == nil {
		t.Fatal("trojan-path accept failed")
	}
	defer conn2.Close()

	// 回放字节必须逐字节等于嗅探期消费的首包
	buf := make([]byte, len(firstPacket))
	if err := readFull(conn2, buf); err != nil {
		t.Fatalf("replayed first packet: %v", err)
	}
	if string(buf) != string(firstPacket) {
		t.Fatalf("replayed bytes mismatch:\n got %q\nwant %q", buf, firstPacket)
	}
	if !util.CheckConn(conn1, conn2) {
		t.Fatal("bidirectional stream broken after replay")
	}
}

// TestSniffHTTPGoesToWebsocketPath 首包是合法 HTTP 请求 → 必须走 WebSocket
// 路径（wsChan，AcceptConn(&websocket.Tunnel{})），HTTP 请求字节经 rewind
// 回放完整交付。
func TestSniffHTTPGoesToWebsocketPath(t *testing.T) {
	f := newSniffTestFixture(t)
	// 必须先注册 websocket overlay：nextHTTP 未置位时 HTTP 请求会被
	// 交给 redirector 丢弃
	accepted := f.acceptAsync(&websocket.Tunnel{})

	conn1, err := f.client.DialConn(nil, nil)
	common.Must(err)
	defer conn1.Close()
	httpReq := "GET /websocket HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
	common.Must2(conn1.Write([]byte(httpReq)))

	var conn2 tunnel.Conn
	select {
	case conn2 = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("websocket-path accept timed out")
	}
	if conn2 == nil {
		t.Fatal("websocket-path accept failed")
	}
	defer conn2.Close()

	buf := make([]byte, len(httpReq))
	if err := readFull(conn2, buf); err != nil {
		t.Fatalf("replayed http request: %v", err)
	}
	if string(buf) != httpReq {
		t.Fatalf("replayed http bytes mismatch:\n got %q\nwant %q", buf, httpReq)
	}
}

// TestSniffNonTLSFallsBack 非 TLS 首包（明文 HTTP 打到 TLS 端口）→ 握手
// 失败回落路径：连接被 rewind 后丢弃/关闭，服务端不崩溃且 trojan 路径
// 后续仍可用（回落分支的 rewind 回放同样受缓冲释放改动影响）。
func TestSniffNonTLSFallsBack(t *testing.T) {
	f := newSniffTestFixture(t)

	plain, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", f.port))
	common.Must(err)
	common.Must2(plain.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\nsome body")))
	plain.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp := make([]byte, 128)
	for {
		_, rerr := plain.Read(resp)
		if rerr != nil {
			break // 服务端已读走并关闭（无 fallback 地址时静默丢弃）
		}
	}
	plain.Close()

	// 回落处理后服务端仍正常服务 trojan 路径
	accepted := f.acceptAsync(nil)
	conn1, err := f.client.DialConn(nil, nil)
	common.Must(err)
	defer conn1.Close()
	common.Must2(conn1.Write([]byte("0123456789abcdef\r\n")))
	select {
	case conn2 := <-accepted:
		if conn2 == nil {
			t.Fatal("trojan-path accept failed after fallback")
		}
		defer conn2.Close()
		buf := make([]byte, 18)
		if err := readFull(conn2, buf); err != nil {
			t.Fatalf("trojan path broken after fallback: %v", err)
		}
		if string(buf) != "0123456789abcdef\r\n" {
			t.Fatalf("unexpected payload %q", buf)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("trojan-path accept timed out after fallback")
	}
}
