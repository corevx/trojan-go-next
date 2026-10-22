package mux

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/xtaci/smux"
)

type muxID uint32

func generateMuxID() muxID {
	return muxID(rand.Uint32())
}

type smuxClientInfo struct {
	id             muxID
	client         *smux.Session
	lastActiveTime time.Time
	underlayConn   tunnel.Conn
	// pendingStreams counts stream slots reserved by DialConn whose Open()
	// has not finished yet. Guarded by Client.clientPoolLock. Together with
	// client.NumStreams() it keeps the concurrency cap exact now that
	// Open() runs outside the pool lock.
	pendingStreams int
}

// Client is a smux client
type Client struct {
	clientPoolLock sync.Mutex
	clientPool     map[muxID]*smuxClientInfo
	underlay       tunnel.Client
	concurrency    int
	timeout        time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	// closed is guarded by clientPoolLock. It stops a dial whose underlay
	// handshake was already in flight when Close() ran from publishing a
	// fresh client into the pool after Close() has drained it.
	closed bool
}

func (c *Client) Close() error {
	c.cancel()
	c.clientPoolLock.Lock()
	defer c.clientPoolLock.Unlock()
	c.closed = true
	for id, info := range c.clientPool {
		info.client.Close()
		log.Debug("mux client", id, "closed")
	}
	return nil
}

func (c *Client) cleanLoop() {
	var checkDuration time.Duration
	if c.timeout <= 0 {
		checkDuration = time.Second * 10
		log.Warn("negative mux timeout")
	} else {
		checkDuration = c.timeout / 4
	}
	log.Debug("check duration:", checkDuration.Seconds(), "s")
	for {
		select {
		case <-time.After(checkDuration):
			c.clientPoolLock.Lock()
			for id, info := range c.clientPool {
				if info.client.IsClosed() {
					info.client.Close()
					info.underlayConn.Close()
					delete(c.clientPool, id)
					log.Info("mux client", id, "is dead")
				} else if info.client.NumStreams()+info.pendingStreams == 0 && time.Since(info.lastActiveTime) > c.timeout {
					info.client.Close()
					info.underlayConn.Close()
					delete(c.clientPool, id)
					log.Info("mux client", id, "is closed due to inactivity")
				}
			}
			log.Debug("current mux clients: ", len(c.clientPool))
			for id, info := range c.clientPool {
				log.Debug(fmt.Sprintf("  - %x: %d/%d", id, info.client.NumStreams(), c.concurrency))
			}
			c.clientPoolLock.Unlock()
		case <-c.ctx.Done():
			log.Debug("shutting down mux cleaner..")
			c.clientPoolLock.Lock()
			c.closed = true
			for id, info := range c.clientPool {
				info.client.Close()
				info.underlayConn.Close()
				delete(c.clientPool, id)
				log.Debug("mux client", id, "closed")
			}
			c.clientPoolLock.Unlock()
			return
		}
	}
}

// newMuxClient dials a fresh underlay connection and wraps it into a smux
// session. It performs the slow network handshake (TCP+TLS etc.) and must be
// called WITHOUT holding clientPoolLock. The returned info is not yet visible
// in the pool; the caller publishes it with publishMuxClient.
func (c *Client) newMuxClient() (*smuxClientInfo, error) {
	fakeAddr := &tunnel.Address{
		DomainName:  "MUX_CONN",
		AddressType: tunnel.DomainName,
	}
	conn, err := c.underlay.DialConn(fakeAddr, &Tunnel{})
	if err != nil {
		return nil, common.NewError("mux failed to dial").Base(err)
	}
	conn = NewStickyConn(conn)

	smuxConfig := smux.DefaultConfig()
	// smuxConfig.KeepAliveDisabled = true
	client, _ := smux.Client(conn, smuxConfig)
	info := &smuxClientInfo{
		client:         client,
		underlayConn:   conn,
		id:             generateMuxID(),
		lastActiveTime: time.Now(),
	}
	return info, nil
}

// publishMuxClient inserts a freshly created client into the pool under a
// unique id and reserves the first stream slot for its creator. It fails if
// the Client has been closed in the meantime.
func (c *Client) publishMuxClient(info *smuxClientInfo) bool {
	c.clientPoolLock.Lock()
	defer c.clientPoolLock.Unlock()
	if c.closed {
		return false
	}
	for {
		if _, found := c.clientPool[info.id]; !found {
			break
		}
		info.id = generateMuxID()
	}
	info.pendingStreams = 1
	c.clientPool[info.id] = info
	return true
}

// reserveStreamSlot scans the pool for a client with spare capacity and
// reserves one stream slot on it, removing dead clients along the way.
// It must be called with clientPoolLock held.
func (c *Client) reserveStreamSlot() *smuxClientInfo {
	for id, info := range c.clientPool {
		if info.client.IsClosed() {
			delete(c.clientPool, id)
			log.Info(fmt.Sprintf("Mux client %x is closed", id))
			continue
		}
		if c.concurrency <= 0 || info.client.NumStreams()+info.pendingStreams < c.concurrency {
			info.pendingStreams++
			return info
		}
	}
	return nil
}

// openStream opens a smux stream on info outside the pool lock and keeps the
// bookkeeping consistent. On failure the client is torn down and evicted from
// the pool, mirroring the original error path.
func (c *Client) openStream(info *smuxClientInfo) (tunnel.Conn, error) {
	rwc, err := info.client.Open()

	c.clientPoolLock.Lock()
	info.pendingStreams--
	info.lastActiveTime = time.Now()
	if err != nil {
		info.underlayConn.Close()
		info.client.Close()
		delete(c.clientPool, info.id)
	}
	c.clientPoolLock.Unlock()

	if err != nil {
		return nil, common.NewError("mux failed to open stream from client").Base(err)
	}
	return &Conn{
		rwc:  rwc,
		Conn: info.underlayConn,
	}, nil
}

func (c *Client) DialConn(*tunnel.Address, tunnel.Tunnel) (tunnel.Conn, error) {
	// Fast path: reuse a pooled client. Only pool bookkeeping happens under
	// the lock; Open() itself is cheap (the SYN header is buffered in memory
	// by stickyConn) and runs without the lock.
	c.clientPoolLock.Lock()
	if c.closed {
		c.clientPoolLock.Unlock()
		return nil, common.NewError("mux client is closed")
	}
	info := c.reserveStreamSlot()
	c.clientPoolLock.Unlock()

	if info == nil {
		// Slow path: every pooled client is at capacity. Establish a new
		// smux client outside the lock so concurrent dialers are not
		// serialized behind the underlay handshake. Concurrent callers may
		// create redundant clients; each is immediately used by its creator
		// and stays in the pool for later reuse.
		var err error
		info, err = c.newMuxClient()
		if err != nil {
			return nil, common.NewError("no available mux client found").Base(err)
		}
		if !c.publishMuxClient(info) {
			info.underlayConn.Close()
			info.client.Close()
			return nil, common.NewError("mux client is closed")
		}
	}
	return c.openStream(info)
}

func (c *Client) DialPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	panic("not supported")
}

func NewClient(ctx context.Context, underlay tunnel.Client) (*Client, error) {
	clientConfig := config.FromContext(ctx, Name).(*Config)
	ctx, cancel := context.WithCancel(ctx)
	client := &Client{
		underlay:    underlay,
		concurrency: clientConfig.Mux.Concurrency,
		timeout:     time.Duration(clientConfig.Mux.IdleTimeout) * time.Second,
		ctx:         ctx,
		cancel:      cancel,
		clientPool:  make(map[muxID]*smuxClientInfo),
	}
	go client.cleanLoop()
	log.Debug("mux client created")
	return client, nil
}
