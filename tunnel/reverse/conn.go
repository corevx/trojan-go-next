package reverse

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/p4gefau1t/trojan-go/tunnel"
)

// Conn wraps an smux stream as a tunnel.Conn, embedding the underlay
// connection for metadata, in the same way as tunnel/mux/conn.go
type Conn struct {
	rwc io.ReadWriteCloser
	tunnel.Conn
}

func (c *Conn) Read(p []byte) (int, error) {
	return c.rwc.Read(p)
}

func (c *Conn) Write(p []byte) (int, error) {
	return c.rwc.Write(p)
}

func (c *Conn) Close() error {
	return c.rwc.Close()
}

// reverseAddr is the net.Addr of a ReverseListener (the server-side bind port)
type reverseAddr struct {
	network string
	addr    string
}

func (a reverseAddr) Network() string {
	return a.network
}

func (a reverseAddr) String() string {
	return a.addr
}

// idleTimeoutConn pushes the read/write deadline forward on every operation,
// so a connection stalled longer than the timeout is closed instead of leaking
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	if c.timeout > 0 {
		c.SetReadDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Read(p)
}

func (c *idleTimeoutConn) Write(p []byte) (int, error) {
	if c.timeout > 0 {
		c.SetWriteDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Write(p)
}

// relayConn copies both directions until one side finishes or ctx is done
func relayConn(ctx context.Context, a, b net.Conn) {
	errChan := make(chan error, 2)
	copyDir := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		errChan <- err
	}
	go copyDir(a, b)
	go copyDir(b, a)
	select {
	case <-errChan:
	case <-ctx.Done():
	}
	a.Close()
	b.Close()
}
