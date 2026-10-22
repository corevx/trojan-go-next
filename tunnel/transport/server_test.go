package transport

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

// flakyListener fails the first N Accept calls with a synthetic transient
// error (EMFILE-shaped net.OpError), then delegates to the wrapped listener.
type flakyListener struct {
	net.Listener
	mu       sync.Mutex
	failures int
}

func (l *flakyListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.failures > 0 {
		l.failures--
		l.mu.Unlock()
		return nil, &net.OpError{
			Op:  "accept",
			Net: "tcp",
			Err: errors.New("too many open files"),
		}
	}
	l.mu.Unlock()
	return l.Listener.Accept()
}

// countingListener counts every Accept call, including failing ones.
type countingListener struct {
	net.Listener
	accepts int64
}

func (l *countingListener) Accept() (net.Conn, error) {
	atomic.AddInt64(&l.accepts, 1)
	return l.Listener.Accept()
}

func newTestServer(listener net.Listener) (*Server, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		tcpListener: listener,
		connChan:    make(chan tunnel.Conn, 32),
		wsChan:      make(chan tunnel.Conn, 32),
		ctx:         ctx,
		cancel:      cancel,
	}, cancel
}

func acceptWithTimeout(t *testing.T, s *Server, timeout time.Duration) tunnel.Conn {
	t.Helper()
	type result struct {
		conn tunnel.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := s.AcceptConn(nil)
		ch <- result{conn, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal("AcceptConn failed:", r.err)
		}
		return r.conn
	case <-time.After(timeout):
		t.Fatal("timed out waiting for AcceptConn")
		return nil
	}
}

func TestAcceptLoopNormal(t *testing.T) {
	cfg := &Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  common.PickPort("tcp", "127.0.0.1"),
		RemoteHost: "127.0.0.1",
		RemotePort: common.PickPort("tcp", "127.0.0.1"),
	}
	ctx := config.WithConfig(context.Background(), Name, cfg)
	s, err := NewServer(ctx, nil)
	common.Must(err)
	defer s.Close()

	conn, err := net.Dial("tcp", net.JoinHostPort(cfg.LocalHost, strconv.Itoa(cfg.LocalPort)))
	common.Must(err)
	defer conn.Close()
	_, err = conn.Write([]byte("hello"))
	common.Must(err)

	accepted := acceptWithTimeout(t, s, 5*time.Second)
	accepted.Close()
}

func TestAcceptLoopSurvivesTransientError(t *testing.T) {
	realListener, err := net.Listen("tcp", "127.0.0.1:0")
	common.Must(err)
	flaky := &flakyListener{Listener: realListener, failures: 3}
	s, cancel := newTestServer(flaky)
	defer cancel()
	go s.acceptLoop()

	// the first 3 Accept calls fail transiently; the loop must retry and pick
	// up this connection once the synthetic failures are exhausted
	conn1, err := net.Dial("tcp", realListener.Addr().String())
	common.Must(err)
	defer conn1.Close()
	accepted1 := acceptWithTimeout(t, s, 5*time.Second)
	accepted1.Close()

	// a second connection proves the loop is alive and accepting again
	conn2, err := net.Dial("tcp", realListener.Addr().String())
	common.Must(err)
	defer conn2.Close()
	accepted2 := acceptWithTimeout(t, s, 5*time.Second)
	accepted2.Close()

	flaky.mu.Lock()
	remaining := flaky.failures
	flaky.mu.Unlock()
	if remaining != 0 {
		t.Fatal("synthetic failures were not all consumed")
	}
}

func TestAcceptLoopExitsOnListenerClose(t *testing.T) {
	realListener, err := net.Listen("tcp", "127.0.0.1:0")
	common.Must(err)
	counter := &countingListener{Listener: realListener}
	s, cancel := newTestServer(counter)
	defer cancel()
	go s.acceptLoop()

	time.Sleep(50 * time.Millisecond) // let the loop enter Accept
	common.Must(counter.Close())

	time.Sleep(100 * time.Millisecond)
	n1 := atomic.LoadInt64(&counter.accepts)
	time.Sleep(300 * time.Millisecond)
	n2 := atomic.LoadInt64(&counter.accepts)
	if n2 != n1 {
		t.Fatalf("accept loop still polling after listener close: %d -> %d Accept calls", n1, n2)
	}
}
