package trojan

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

// startFlushStack 起一个裸 TCP listener 作为被拨端，返回可拨它的 trojan
// 客户端与接收端。验证头部延迟冲出语义：应用层不写首包时，头部应在
// headerFlushDelay 后自动发出。
func startFlushStack(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	common.Must(err)
	t.Cleanup(func() { _ = ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	ctx := config.WithConfig(context.Background(), transport.Name, &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	})
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	ctx = config.WithConfig(ctx, memory.Name, &memory.Config{Passwords: []string{"password"}})
	ctx = config.WithConfig(ctx, Name, &Config{RemoteHost: "127.0.0.1", RemotePort: port})
	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	c, err := NewClient(ctx, tcpClient)
	common.Must(err)
	t.Cleanup(func() { _ = c.Close() })

	type accepted struct {
		conn net.Conn
		err  error
	}
	ch := make(chan accepted, 1)
	go func() {
		conn, err := ln.Accept()
		ch <- accepted{conn, err}
	}()

	conn, err := c.DialConn(&tunnel.Address{
		DomainName:  "example.com",
		AddressType: tunnel.DomainName,
		Port:        443,
	}, nil)
	common.Must(err)
	_ = conn // 不写任何应用数据，观察头部是否自行冲出

	ac := <-ch
	common.Must(ac.err)
	t.Cleanup(func() { _ = ac.conn.Close() })
	return c, ac.conn
}

// TestHeaderFlushAfterDelay 应用层一直不写首包时，trojan 头须在
// headerFlushDelay 后自动冲出（对端可能等服务端先发数据，头不发出对端
// 永远读不到请求元数据）。
func TestHeaderFlushAfterDelay(t *testing.T) {
	_, server := startFlushStack(t)

	server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := [128]byte{}
	n, err := server.Read(buf[:])
	if err != nil || n == 0 {
		t.Fatalf("trojan header should be flushed without app data, got n=%d err=%v", n, err)
	}
	if n >= 2 && buf[n-2] == 0x0d && buf[n-1] == 0x0a {
		return // 以 CRLF 结尾的 trojan 头
	}
	// 头可能分多次到达，继续读到 CRLF 为止
	for i := 0; i < 8; i++ {
		m, err := server.Read(buf[n:])
		n += m
		if err != nil {
			t.Fatalf("read error while collecting header: %v", err)
		}
		if n >= 2 && buf[n-2] == 0x0d && buf[n-1] == 0x0a {
			return
		}
	}
	t.Fatalf("no CRLF-terminated trojan header observed, got %d bytes", n)
}

// TestHeaderFlushCancelledByClose 客户端在延迟窗口内关闭时，等待中的
// flush 必须被取消：对端不得再收到头部（旧实现 time.Sleep 不可取消，
// 关闭后仍会向已关连接写头）。
func TestHeaderFlushCancelledByClose(t *testing.T) {
	c, server := startFlushStack(t)

	common.Must(c.Close()) // 在 100ms 窗口内关闭
	server.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := [128]byte{}
	n, err := server.Read(buf[:])
	if n > 0 {
		t.Fatalf("cancelled client must not flush the header, got %d bytes", n)
	}
	if err == nil {
		t.Fatal("expected timeout or EOF after cancellation")
	}
	if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		// EOF 也合法：underlay 关闭顺带断开 TCP
		if err.Error() != "EOF" {
			t.Fatalf("unexpected read error: %v", err)
		}
	}
}
