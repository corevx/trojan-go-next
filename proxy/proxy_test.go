package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

// captureLogger records log calls so tests can assert log levels.
type captureLogger struct {
	mu     sync.Mutex
	errors []string
	debugs []string
}

func (l *captureLogger) record(level string, v ...interface{}) {
	msg := fmt.Sprint(v...)
	l.mu.Lock()
	defer l.mu.Unlock()
	switch level {
	case "error":
		l.errors = append(l.errors, msg)
	case "debug":
		l.debugs = append(l.debugs, msg)
	}
}

func (l *captureLogger) snapshots() (errs, debugs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.errors...), append([]string{}, l.debugs...)
}

func (l *captureLogger) Fatal(v ...interface{})                 { l.record("fatal", v...) }
func (l *captureLogger) Fatalf(format string, v ...interface{}) {}
func (l *captureLogger) Error(v ...interface{})                 { l.record("error", v...) }
func (l *captureLogger) Errorf(format string, v ...interface{}) {
	l.record("error", fmt.Sprintf(format, v...))
}
func (l *captureLogger) Warn(v ...interface{})                 {}
func (l *captureLogger) Warnf(format string, v ...interface{}) {}
func (l *captureLogger) Info(v ...interface{})                 {}
func (l *captureLogger) Infof(format string, v ...interface{}) {}
func (l *captureLogger) Debug(v ...interface{})                { l.record("debug", v...) }
func (l *captureLogger) Debugf(format string, v ...interface{}) {
	l.record("debug", fmt.Sprintf(format, v...))
}
func (l *captureLogger) Trace(v ...interface{})                 {}
func (l *captureLogger) Tracef(format string, v ...interface{}) {}
func (l *captureLogger) SetLogLevel(log.LogLevel)               {}
func (l *captureLogger) SetOutput(w io.Writer)                  {}

// scriptedRead is one ReadWithMetadata result of a stream-backed PacketConn.
type scriptedRead struct {
	payload  []byte
	metadata *tunnel.Metadata
	err      error
}

type recordedWrite struct {
	payload  []byte
	metadata *tunnel.Metadata
}

// mockStreamPacketConn simulates a stream-backed inbound PacketConn (e.g. the
// trojan server side): scripted reads, recorded writes, Close is observable.
type mockStreamPacketConn struct {
	mu        sync.Mutex
	reads     []scriptedRead
	writes    []recordedWrite
	closed    chan struct{}
	closeOnce sync.Once
}

func newMockStreamPacketConn(reads []scriptedRead) *mockStreamPacketConn {
	return &mockStreamPacketConn{reads: reads, closed: make(chan struct{})}
}

func (c *mockStreamPacketConn) ReadWithMetadata(p []byte) (int, *tunnel.Metadata, error) {
	c.mu.Lock()
	if len(c.reads) == 0 {
		c.mu.Unlock()
		return 0, nil, io.EOF
	}
	r := c.reads[0]
	c.reads = c.reads[1:]
	c.mu.Unlock()
	if r.err != nil {
		return 0, nil, r.err
	}
	copy(p, r.payload)
	return len(r.payload), r.metadata, nil
}

func (c *mockStreamPacketConn) WriteWithMetadata(p []byte, m *tunnel.Metadata) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, recordedWrite{append([]byte{}, p...), m})
	c.mu.Unlock()
	return len(p), nil
}

func (c *mockStreamPacketConn) snapshotWrites() []recordedWrite {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]recordedWrite{}, c.writes...)
}

func (c *mockStreamPacketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *mockStreamPacketConn) ReadFrom(p []byte) (int, net.Addr, error)     { return 0, nil, io.EOF }
func (c *mockStreamPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) { return len(p), nil }
func (c *mockStreamPacketConn) LocalAddr() net.Addr                          { return nil }
func (c *mockStreamPacketConn) SetDeadline(t time.Time) error                { return nil }
func (c *mockStreamPacketConn) SetReadDeadline(t time.Time) error            { return nil }
func (c *mockStreamPacketConn) SetWriteDeadline(t time.Time) error           { return nil }

// mockUDPPacketConn simulates the outbound sink: writes are recorded and
// signalled, reads block until Close, like a real UDP socket.
type mockUDPPacketConn struct {
	mockStreamPacketConn
	written chan struct{}
}

func newMockUDPPacketConn() *mockUDPPacketConn {
	return &mockUDPPacketConn{
		mockStreamPacketConn: mockStreamPacketConn{closed: make(chan struct{})},
		written:              make(chan struct{}, 16),
	}
}

func (c *mockUDPPacketConn) ReadWithMetadata(p []byte) (int, *tunnel.Metadata, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}

func (c *mockUDPPacketConn) WriteWithMetadata(p []byte, m *tunnel.Metadata) (int, error) {
	n, err := c.mockStreamPacketConn.WriteWithMetadata(p, m)
	c.written <- struct{}{}
	return n, err
}

type mockPacketServer struct {
	packetCh  chan tunnel.PacketConn
	closed    chan struct{}
	closeOnce sync.Once
}

func newMockPacketServer(pc tunnel.PacketConn) *mockPacketServer {
	s := &mockPacketServer{packetCh: make(chan tunnel.PacketConn, 1), closed: make(chan struct{})}
	s.packetCh <- pc
	return s
}

func (s *mockPacketServer) AcceptConn(tunnel.Tunnel) (tunnel.Conn, error) {
	<-s.closed
	return nil, net.ErrClosed
}

func (s *mockPacketServer) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	select {
	case pc := <-s.packetCh:
		return pc, nil
	case <-s.closed:
		return nil, net.ErrClosed
	}
}

func (s *mockPacketServer) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

type mockSink struct {
	pc *mockUDPPacketConn
}

func (s *mockSink) DialConn(*tunnel.Address, tunnel.Tunnel) (tunnel.Conn, error) {
	return nil, errors.New("unsupported in packet relay test")
}

func (s *mockSink) DialPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	return s.pc, nil
}

func (s *mockSink) Close() error { return s.pc.Close() }

func TestIsPacketSessionClose(t *testing.T) {
	resetErr := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"raw io.EOF", io.EOF, true},
		{"production EOF chain", common.NewError("failed to parse udp packet addr").Base(common.NewError("unable to read ATYP").Base(io.EOF)), true},
		{"raw net.ErrClosed", net.ErrClosed, true},
		{"flattened net.ErrClosed", common.NewError("failed to read crlf").Base(net.ErrClosed), true},
		{"raw ECONNRESET", syscall.ECONNRESET, true},
		{"opError ECONNRESET", resetErr, true},
		{"flattened ECONNRESET", common.NewError("failed to parse udp packet addr").Base(resetErr), true},
		{"raw unexpected EOF is a truncated frame", io.ErrUnexpectedEOF, false},
		{"flattened unexpected EOF is a truncated frame", common.NewError("failed to read payload").Base(io.ErrUnexpectedEOF), false},
		{"protocol violation stays an error", common.NewError("unable to read ATYP").Base(common.NewError("invalid ATYP 99")), false},
		{"generic error", errors.New("something else"), false},
	}
	for _, c := range cases {
		if got := isPacketSessionClose(c.err); got != c.want {
			t.Errorf("%s: isPacketSessionClose = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRelayPacketLoopSessionClose drives relayPacketLoop end to end: a valid
// packet must be relayed, and the peer closing the stream-backed session
// (io.EOF chain) must be logged at debug level, not error.
func TestRelayPacketLoopSessionClose(t *testing.T) {
	productionEOF := common.NewError("failed to parse udp packet addr").
		Base(common.NewError("unable to read ATYP").Base(io.EOF))
	runRelaySessionClose(t, []scriptedRead{
		{payload: []byte("dns-query"), metadata: &tunnel.Metadata{Address: tunnel.NewAddressFromHostPort("udp", "8.8.8.8", 53)}},
		{err: productionEOF},
	}, true)
	// immediate close, no payload: raw io.EOF exercises the errors.Is path
	runRelaySessionClose(t, []scriptedRead{{err: io.EOF}}, false)
}

func runRelaySessionClose(t *testing.T, script []scriptedRead, expectPacket bool) {
	cl := &captureLogger{}
	log.RegisterLogger(cl)
	defer log.RegisterLogger(&log.EmptyLogger{})

	inbound := newMockStreamPacketConn(script)
	outbound := newMockUDPPacketConn()
	server := newMockPacketServer(inbound)
	sink := &mockSink{pc: outbound}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewProxy(ctx, cancel, []tunnel.Server{server}, sink)
	go func() { _ = p.Run() }()
	defer p.Close()

	relayed := false
	if expectPacket {
		select {
		case <-outbound.written:
			relayed = true
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for packet to be relayed")
		}
	}
	select {
	case <-inbound.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for relay teardown on session close")
	}

	errs, debugs := cl.snapshots()
	if len(errs) != 0 {
		t.Fatalf("session close must not be logged at error level, got: %v", errs)
	}
	if expectPacket && !relayed {
		t.Fatal("expected one relayed packet before close")
	}
	found := false
	for _, d := range debugs {
		if strings.Contains(d, "session close") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a debug log describing the session close, debugs: %v", debugs)
	}
	writes := outbound.snapshotWrites()
	if expectPacket {
		if len(writes) != 1 || string(writes[0].payload) != "dns-query" {
			t.Errorf("expected exactly one relayed packet, got %d writes", len(writes))
		}
		if len(writes) == 1 && writes[0].metadata == nil {
			t.Error("relayed packet lost its metadata")
		}
	} else if len(writes) != 0 {
		t.Errorf("expected no relayed packets, got %d writes", len(writes))
	}

	// Shut down and wait for both accept loops to finish their final
	// log.Debug("exiting") call before returning: the deferred
	// log.RegisterLogger restore writes the log package global without a
	// lock, so it must not overlap with relay goroutines still logging.
	p.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, debugs := cl.snapshots()
		exits := 0
		for _, d := range debugs {
			if strings.Contains(d, "exiting") {
				exits++
			}
		}
		if exits >= 2 || time.Now().After(deadline) {
			if exits < 2 {
				t.Fatalf("timeout waiting for accept loops to exit, debugs: %v", debugs)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
