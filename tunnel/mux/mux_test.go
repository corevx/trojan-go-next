package mux

import (
	"context"
	"encoding/binary"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/test/util"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

func TestMux(t *testing.T) {
	muxCfg := &Config{
		Mux: MuxConfig{
			Enabled:     true,
			Concurrency: 8,
			IdleTimeout: 60,
		},
	}
	ctx := config.WithConfig(context.Background(), Name, muxCfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx = config.WithConfig(ctx, transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})

	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)

	common.Must(err)

	muxTunnel := Tunnel{}
	muxClient, _ := muxTunnel.NewClient(ctx, tcpClient)
	muxServer, _ := muxTunnel.NewServer(ctx, tcpServer)

	conn1, err := muxClient.DialConn(nil, nil)
	common.Must2(conn1.Write(util.GeneratePayload(1024)))
	common.Must(err)
	buf := [1024]byte{}
	conn2, err := muxServer.AcceptConn(nil)
	common.Must(err)
	common.Must2(conn2.Read(buf[:]))
	if !util.CheckConn(conn1, conn2) {
		t.Fail()
	}
	conn1.Close()
	conn2.Close()
	muxClient.Close()
	muxServer.Close()
}

// slowDialClient wraps a tunnel.Client and delays every underlay dial,
// simulating a slow (TCP+TLS) handshake so that lock contention inside mux
// DialConn becomes observable.
type slowDialClient struct {
	tunnel.Client
	delay time.Duration
	dials int32
}

func (s *slowDialClient) DialConn(addr *tunnel.Address, t tunnel.Tunnel) (tunnel.Conn, error) {
	atomic.AddInt32(&s.dials, 1)
	time.Sleep(s.delay)
	return s.Client.DialConn(addr, t)
}

// newMuxTestStack brings up a mux client and server over real loopback TCP.
// A non-nil slow wrapper is inserted between the mux client and the transport
// client to delay underlay dials.
func newMuxTestStack(t *testing.T, slow *slowDialClient, concurrency int) (context.Context, *Client, *Server) {
	t.Helper()
	muxCfg := &Config{
		Mux: MuxConfig{
			Enabled:     true,
			Concurrency: concurrency,
			IdleTimeout: 60,
		},
	}
	ctx := config.WithConfig(context.Background(), Name, muxCfg)

	port := common.PickPort("tcp", "127.0.0.1")
	transportConfig := &transport.Config{
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: port,
	}
	ctx = config.WithConfig(ctx, transport.Name, transportConfig)
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})

	tcpClient, err := transport.NewClient(ctx, nil)
	common.Must(err)
	var underlay tunnel.Client = tcpClient
	if slow != nil {
		slow.Client = tcpClient
		underlay = slow
	}
	tcpServer, err := transport.NewServer(ctx, nil)
	common.Must(err)

	muxTunnel := Tunnel{}
	muxClient, err := muxTunnel.NewClient(ctx, underlay)
	common.Must(err)
	muxServer, err := muxTunnel.NewServer(ctx, tcpServer)
	common.Must(err)
	return ctx, muxClient.(*Client), muxServer.(*Server)
}

// exchangeTags writes a 4-byte tag on every client conn and verifies the mux
// server receives exactly the same set of tags, proving every dialed stream
// carries data end to end.
func exchangeTags(t *testing.T, muxServer *Server, conns []tunnel.Conn, timeout time.Duration) {
	t.Helper()
	tags := make(chan uint32, len(conns))
	go func() {
		defer close(tags)
		for range conns {
			serverConn, err := muxServer.AcceptConn(nil)
			if err != nil {
				return
			}
			buf := make([]byte, 4)
			if _, err := io.ReadFull(serverConn, buf); err != nil {
				serverConn.Close()
				return
			}
			serverConn.Close()
			tags <- binary.LittleEndian.Uint32(buf)
		}
	}()
	for i, conn := range conns {
		tag := make([]byte, 4)
		binary.LittleEndian.PutUint32(tag, uint32(i))
		if _, err := conn.Write(tag); err != nil {
			t.Fatal("failed to write tag:", err)
		}
	}
	received := make(map[uint32]bool)
	deadline := time.After(timeout)
	for {
		select {
		case tag, ok := <-tags:
			if !ok {
				if len(received) != len(conns) {
					t.Fatalf("expected %d distinct tags on the server side, got %v", len(conns), received)
				}
				return
			}
			received[tag] = true
		case <-deadline:
			t.Fatalf("timed out exchanging tags, received %v", received)
		}
	}
}

// TestMuxConcurrentDial proves that concurrent DialConn calls are not
// serialized behind the underlay handshake: with concurrency=1 two dialers
// must each establish their own mux client, which used to happen strictly
// sequentially under the global pool lock (>= 2*delay) and now overlaps.
func TestMuxConcurrentDial(t *testing.T) {
	const delay = 500 * time.Millisecond
	slow := &slowDialClient{delay: delay}
	_, muxClient, muxServer := newMuxTestStack(t, slow, 1)
	defer muxClient.Close()
	defer muxServer.Close()

	const dialers = 2
	conns := make([]tunnel.Conn, dialers)
	errs := make([]error, dialers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < dialers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			conns[i], errs[i] = muxClient.DialConn(nil, nil)
		}(i)
	}
	begin := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(begin)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("dialer %d failed: %v", i, err)
		}
	}
	if dials := atomic.LoadInt32(&slow.dials); dials != dialers {
		t.Fatalf("expected %d underlay dials (one per mux client), got %d", dialers, dials)
	}
	// Serial execution of two handshakes costs at least 2*delay; parallel
	// execution about one delay. Fail if the old serialization is back.
	if elapsed >= 2*delay*80/100 {
		t.Fatalf("concurrent DialConn looks serialized: %v elapsed for %d dials of %v each", elapsed, dialers, delay)
	}
	t.Logf("2 concurrent dials with %v handshake finished in %v", delay, elapsed)

	exchangeTags(t, muxServer, conns, 5*time.Second)
	for _, conn := range conns {
		conn.Close()
	}
}

// TestMuxPoolReuse proves the idle-pool reuse semantics survive the lock
// rework: when the pooled client still has free slots, concurrent dialers
// share it instead of creating redundant mux clients.
func TestMuxPoolReuse(t *testing.T) {
	slow := &slowDialClient{}
	_, muxClient, muxServer := newMuxTestStack(t, slow, 8)
	defer muxClient.Close()
	defer muxServer.Close()

	first, err := muxClient.DialConn(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dials := atomic.LoadInt32(&slow.dials); dials != 1 {
		t.Fatalf("expected 1 underlay dial, got %d", dials)
	}

	const dialers = 2
	conns := make([]tunnel.Conn, 1+dialers)
	conns[0] = first
	errs := make([]error, dialers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < dialers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			conns[i+1], errs[i] = muxClient.DialConn(nil, nil)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("dialer %d failed: %v", i, err)
		}
	}
	if dials := atomic.LoadInt32(&slow.dials); dials != 1 {
		t.Fatalf("expected pooled client reuse (still 1 underlay dial), got %d", dials)
	}

	exchangeTags(t, muxServer, conns, 5*time.Second)
	for _, conn := range conns {
		conn.Close()
	}
}
