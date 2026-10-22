package websocket

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

func TestWebsocket(t *testing.T) {
	cfg := &Config{
		Websocket: WebsocketConfig{
			Enabled: true,
			Host:    "localhost",
			Path:    "/ws",
		},
	}

	ctx := config.WithConfig(context.Background(), Name, cfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	freedomCfg := &freedom.Config{}
	ctx = config.WithConfig(ctx, transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, freedomCfg)
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)

	c, err := NewClient(ctx, tcpClient)
	common.Must(err)
	s, err := NewServer(ctx, tcpServer)
	var conn2 tunnel.Conn
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// 局部变量收错误，避免与主流程对共享 err 的写并发（-race 报警）
		var acceptErr error
		conn2, acceptErr = s.AcceptConn(nil)
		common.Must(acceptErr)
		wg.Done()
	}()
	time.Sleep(time.Second)
	conn1, err := c.DialConn(nil, nil)
	common.Must(err)
	wg.Wait()
	if !util.CheckConn(conn1, conn2) {
		t.Fail()
	}

	if strings.HasPrefix(conn1.RemoteAddr().String(), "ws") {
		t.Fail()
	}
	if strings.HasPrefix(conn2.RemoteAddr().String(), "ws") {
		t.Fail()
	}

	conn1.Close()
	conn2.Close()
	s.Close()
	c.Close()
}

func TestRedirect(t *testing.T) {
	cfg := &Config{
		RemoteHost: "127.0.0.1",
		Websocket: WebsocketConfig{
			Enabled: true,
			Host:    "localhost",
			Path:    "/ws",
		},
	}
	fmt.Sscanf(util.HTTPPort, "%d", &cfg.RemotePort)
	ctx := config.WithConfig(context.Background(), Name, cfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost: "127.0.0.1",
		LocalPort: port,
	}
	ctx = config.WithConfig(ctx, transport.Name, transportConfig)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)

	s, err := NewServer(ctx, tcpServer)
	common.Must(err)

	go func() {
		_, err := s.AcceptConn(nil)
		if err == nil {
			t.Fail()
		}
	}()
	time.Sleep(time.Second)
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	common.Must(err)
	url := "wss://localhost/wrong-path"
	origin := "https://localhost"
	wsConfig, err := websocket.NewConfig(url, origin)
	common.Must(err)
	_, err = websocket.NewClient(wsConfig, conn)
	if err == nil {
		t.Fail()
	}
	conn.Close()

	s.Close()
}

// TestMalformedHandshakeNoStall 缺陷回归（P0）：带 Upgrade 头 + 正确 path 但缺
// Sec-WebSocket-Key 的畸形请求，x/net/websocket 的 ServeHTTP 写完 400 直接返回、
// 不调用 Handler。修复前 AcceptConn 收不到任何信号，只能等满随机 timeout(5-15s)；
// 上层 acceptLoop 串行调用下低速率畸形请求即可长期占死 ws 接受路径。修复后
// ServeHTTP 退出立即解除等待：5 条畸形请求总耗时应远小于 N×timeout 下限(25s)。
func TestMalformedHandshakeNoStall(t *testing.T) {
	cfg := &Config{
		Websocket: WebsocketConfig{
			Enabled: true,
			Host:    "localhost",
			Path:    "/ws",
		},
	}
	ctx := config.WithConfig(context.Background(), Name, cfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx = config.WithConfig(ctx, transport.Name, transportConfig)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)
	s, err := NewServer(ctx, tcpServer)
	common.Must(err)
	defer s.Close()

	const n = 5
	acceptDone := make(chan error, 1)
	go func() {
		for i := 0; i < n; i++ {
			if _, err := s.AcceptConn(nil); err == nil {
				acceptDone <- fmt.Errorf("malformed request %d unexpectedly accepted", i)
				return
			}
		}
		acceptDone <- nil
	}()

	start := time.Now()
	for i := 0; i < n; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		common.Must(err)
		// Upgrade + 正确 path，但缺 Sec-WebSocket-Key / Sec-WebSocket-Version
		fmt.Fprint(conn, "GET /ws HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		// 读走服务端 400 响应直至 EOF（ServeHTTP 返回时会关闭连接）
		io.Copy(io.Discard, conn)
		conn.Close()
	}
	elapsed := time.Since(start)

	select {
	case err := <-acceptDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("AcceptConn stalled on malformed handshake requests")
	}

	// 修复前每条至少等满 5s（timeout 随机下限），5 条 >= 25s；
	// 修复后毫秒级返回，15s 上限有充分余量。
	if elapsed > 15*time.Second {
		t.Fatalf("%d malformed handshakes took %v, stall regression (want < 15s)", n, elapsed)
	}
	t.Logf("%d malformed handshakes processed in %v", n, elapsed)
}
