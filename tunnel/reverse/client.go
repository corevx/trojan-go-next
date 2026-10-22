package reverse

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/mux"
	"github.com/xtaci/smux"
)

const (
	// bindTimeout bounds the BindReq/BindResp handshake on the control stream
	bindTimeout = time.Second * 60 // CI 弱核(2C)并行全量测试时 30s 仍偶发超时(TestReverseNoGoroutineLeak 实测 31.02s 卡线):成功路径即时返回,只放宽失败判定窗
	// controlWriteWait bounds a single control frame write
	controlWriteWait    = time.Second * 5
	defaultRetryInitial = time.Second * 3
	defaultRetryMax     = time.Second * 60
)

// RetryPolicy controls the automatic rebinding backoff after a session loss:
// exponential growth from Initial to Max, with jitter on every attempt
type RetryPolicy struct {
	Initial time.Duration
	Max     time.Duration
}

type dialOptions struct {
	target string
	retry  RetryPolicy
}

// DialOption tunes the behavior of Client.DialReverse
type DialOption func(*dialOptions)

// WithTarget makes the listener relay every accepted connection to target (host:port)
func WithTarget(target string) DialOption {
	return func(o *dialOptions) {
		o.target = target
	}
}

// WithRetry overrides the reconnect backoff policy
func WithRetry(policy RetryPolicy) DialOption {
	return func(o *dialOptions) {
		o.retry = policy
	}
}

// BindError is a definitive rejection from the server, as opposed to a
// transport failure. Callers (and the reconnect loop) can branch on Code.
type BindError struct {
	Code byte
	Msg  string
}

func (e *BindError) Error() string {
	return fmt.Sprintf("reverse bind rejected: code=%d %s", e.Code, e.Msg)
}

// Permanent means the server will never accept this bind as configured,
// so reconnecting is pointless
func (e *BindError) Permanent() bool {
	return e.Code != CodeInUse && e.Code != CodeLimit
}

// Client is a reverse tunnel client. It asks an authenticated trojan server to
// bind ports on its side and turns the inbound traffic there into local
// connections, effectively a reverse port forward.
type Client struct {
	underlay tunnel.Client
	cfg      *Section
	ctx      context.Context
	cancel   context.CancelFunc

	listeners sync.Map // *ReverseListener -> struct{}
}

func (c *Client) DialConn(*tunnel.Address, tunnel.Tunnel) (tunnel.Conn, error) {
	return nil, common.NewError("reverse client does not support DialConn, use DialReverse")
}

func (c *Client) DialPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	return nil, common.NewError("reverse client does not support DialPacket")
}

func (c *Client) Close() error {
	c.cancel()
	c.listeners.Range(func(key, _ interface{}) bool {
		key.(*ReverseListener).Close()
		return true
	})
	return nil
}

// DialReverse asks the server to bind remotePort and returns a listener whose
// Accept yields one connection per visitor connecting to server:remotePort.
// The first bind attempt is synchronous, so policy rejections (ACL, port in
// use, ...) are returned to the caller; after that, a lost session is rebound
// automatically following the retry policy.
func (c *Client) DialReverse(ctx context.Context, remotePort int, opts ...DialOption) (*ReverseListener, error) {
	options := dialOptions{
		retry: RetryPolicy{
			Initial: defaultRetryInitial,
			Max:     defaultRetryMax,
		},
	}
	for _, opt := range opts {
		opt(&options)
	}
	if remotePort < 1 || remotePort > 65535 {
		return nil, common.NewError("invalid reverse port")
	}
	listenerCtx, cancel := context.WithCancel(ctx)
	listener := &ReverseListener{
		client:     c,
		remotePort: remotePort,
		options:    options,
		connChan:   make(chan net.Conn, 32),
		ctx:        listenerCtx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	if err := listener.connect(); err != nil {
		cancel()
		return nil, err
	}
	c.listeners.Store(listener, struct{}{})
	go listener.maintainLoop()
	if options.target != "" {
		go func() {
			if err := listener.Serve(options.target); err != nil {
				log.Debug("reverse serve ended:", err)
			}
		}()
	}
	return listener, nil
}

func NewClient(ctx context.Context, underlay tunnel.Client) (*Client, error) {
	cfgInterface := config.FromContext(ctx, Name)
	if cfgInterface == nil {
		return nil, common.NewError("reverse config not found")
	}
	ctx, cancel := context.WithCancel(ctx)
	client := &Client{
		underlay: underlay,
		cfg:      &cfgInterface.(*Config).Reverse,
		ctx:      ctx,
		cancel:   cancel,
	}
	log.Debug("reverse client created")
	return client, nil
}

// reverseSession is one live smux session of a listener. dead is closed when
// the session is considered lost by ANY signal: a failed AcceptStream, a
// failed control write/read, or an explicit smux Close. This is required
// because smux only closes its own CloseChan on explicit Close or a keepalive
// timeout, while a dead socket shows up as a read/write error first.
type reverseSession struct {
	session  *smux.Session
	underlay tunnel.Conn
	control  *smux.Stream

	dead     chan struct{}
	deadOnce sync.Once
}

func (s *reverseSession) markDead() {
	s.deadOnce.Do(func() {
		close(s.dead)
	})
}

// ReverseListener accepts connections made to the server-side bound port.
// It survives session loss by rebinding automatically; Done is closed only
// when the listener is closed or rebinding is permanently rejected.
type ReverseListener struct {
	client     *Client
	remotePort int
	options    dialOptions

	mu      sync.Mutex
	current *reverseSession

	// connected mirrors the session liveness for Connected(): true while a
	// session is established, false while rebinding with backoff. It is the
	// health surface an upstream failover watchdog polls — a listener stuck
	// retrying a dead server stays alive (Done not closed) yet never becomes
	// connected, which is exactly the state an orchestrator must treat as
	// "give up on this server, try the next candidate".
	connected atomic.Bool

	connChan  chan net.Conn
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// Connected reports whether the reverse session is currently established
// (bound and not inside a reconnect backoff window). It does not change the
// reconnect semantics in any way; it only exposes liveness for polling.
func (l *ReverseListener) Connected() bool {
	return l.connected.Load()
}

// Accept returns the next connection made to the server-side bound port
func (l *ReverseListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connChan:
		return conn, nil
	case <-l.ctx.Done():
		return nil, common.NewError("reverse listener closed")
	}
}

// Addr returns the server-side bind address of this listener
func (l *ReverseListener) Addr() net.Addr {
	return reverseAddr{
		network: "reverse-tcp",
		addr:    fmt.Sprintf("reverse:%d", l.remotePort),
	}
}

// Done is closed when the listener is closed or permanently rejected
func (l *ReverseListener) Done() <-chan struct{} {
	return l.done
}

// Close unbinds the remote port and tears down the session
func (l *ReverseListener) Close() error {
	l.closeOnce.Do(func() {
		l.connected.Store(false)
		l.cancel()
		l.mu.Lock()
		rs := l.current
		l.current = nil
		l.mu.Unlock()
		if rs != nil {
			rs.markDead()
			// best-effort unbind; the server also releases on session death
			rs.control.SetWriteDeadline(time.Now().Add(controlWriteWait))
			writeFrame(rs.control, frameUnbindReq, (&unbindReq{Port: l.remotePort}).marshal())
			rs.control.Close()
			rs.session.Close()
			rs.underlay.Close()
		}
		close(l.done)
		l.client.listeners.Delete(l)
	})
	return nil
}

// Serve accepts connections and relays each one to target (host:port).
// It blocks until the listener is closed.
func (l *ReverseListener) Serve(target string) error {
	if target == "" {
		return common.NewError("reverse serve target is empty")
	}
	idleTimeout := l.client.cfg.streamIdleDuration()
	for {
		conn, err := l.Accept()
		if err != nil {
			return common.NewError("reverse serve loop ended").Base(err)
		}
		go func(conn net.Conn) {
			defer conn.Close()
			if idleTimeout > 0 {
				conn = &idleTimeoutConn{Conn: conn, timeout: idleTimeout}
			}
			dialer := net.Dialer{}
			outbound, err := dialer.DialContext(l.ctx, "tcp", target)
			if err != nil {
				log.Error(common.NewError("reverse failed to dial target " + target).Base(err))
				return
			}
			defer outbound.Close()
			relayConn(l.ctx, conn, outbound)
		}(conn)
	}
}

// connect dials the underlay, establishes the smux session, performs the
// BindReq/BindResp handshake and starts the per-session goroutines
func (l *ReverseListener) connect() error {
	// the client side plays the smux *server* role: it accepts streams that
	// the trojan server opens for every visitor connecting to the bound port
	conn, err := l.client.underlay.DialConn(&tunnel.Address{
		DomainName:  "REVERSE_CONN",
		Port:        l.remotePort,
		AddressType: tunnel.DomainName,
	}, &Tunnel{})
	if err != nil {
		return common.NewError("reverse failed to dial underlay").Base(err)
	}
	conn = mux.NewStickyConn(conn)

	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxStreamBuffer = l.client.cfg.StreamBuffer
	smuxConfig.MaxReceiveBuffer = l.client.cfg.StreamBuffer
	applyKeepalive(smuxConfig, l.client.cfg.IdleTimeout)
	applyFrameShaping(smuxConfig, *l.client.cfg)
	session, err := smux.Server(conn, smuxConfig)
	if err != nil {
		conn.Close()
		return common.NewError("reverse failed to create smux session").Base(err)
	}
	session.SetDeadline(time.Now().Add(bindTimeout))
	control, err := session.AcceptStream()
	session.SetDeadline(time.Time{})
	if err != nil {
		session.Close()
		conn.Close()
		return common.NewError("reverse failed to accept control stream").Base(err)
	}
	if err := writeFrame(control, frameBindReq, (&bindReq{Port: l.remotePort}).marshal()); err != nil {
		session.Close()
		conn.Close()
		return common.NewError("reverse failed to send bind request").Base(err)
	}
	control.SetReadDeadline(time.Now().Add(bindTimeout))
	frameType, payload, err := readFrame(control)
	control.SetReadDeadline(time.Time{})
	if err != nil {
		session.Close()
		conn.Close()
		return common.NewError("reverse failed to read bind response").Base(err)
	}
	if frameType != frameBindResp {
		session.Close()
		conn.Close()
		return common.NewError("unexpected reverse frame on control stream")
	}
	resp, err := parseBindResp(payload)
	if err != nil {
		session.Close()
		conn.Close()
		return err
	}
	if resp.Code != CodeOK {
		session.Close()
		conn.Close()
		return &BindError{Code: resp.Code, Msg: resp.Msg}
	}

	l.mu.Lock()
	l.current = &reverseSession{
		session:  session,
		underlay: conn,
		control:  control,
		dead:     make(chan struct{}),
	}
	rs := l.current
	l.mu.Unlock()
	l.connected.Store(true)

	go l.acceptLoop(rs)
	go l.keepaliveLoop(rs)
	return nil
}

// teardownSession drops the current session without giving up the listener
func (l *ReverseListener) teardownSession() {
	l.connected.Store(false)
	l.mu.Lock()
	rs := l.current
	l.current = nil
	l.mu.Unlock()
	if rs == nil {
		return
	}
	rs.markDead()
	rs.control.Close()
	rs.session.Close()
	rs.underlay.Close()
}

// maintainLoop watches the session and rebinds with exponential backoff and
// jitter after a session loss, until Close or a permanent rejection
func (l *ReverseListener) maintainLoop() {
	for {
		l.mu.Lock()
		rs := l.current
		l.mu.Unlock()
		if rs == nil {
			select {
			case <-l.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		select {
		case <-l.ctx.Done():
			return
		case <-rs.dead:
		}
		if l.ctx.Err() != nil {
			return
		}
		log.Info("reverse session on port", l.remotePort, "lost, rebinding")
		l.teardownSession()
		delay := l.options.retry.Initial
		for {
			sleep := delay + time.Duration(rand.Int63n(int64(delay/2)+1))
			select {
			case <-l.ctx.Done():
				return
			case <-time.After(sleep):
			}
			err := l.connect()
			if err == nil {
				log.Info("reverse port", l.remotePort, "rebound")
				break
			}
			var be *BindError
			if errors.As(err, &be) && be.Permanent() {
				log.Error(common.NewError("reverse rebinding permanently rejected").Base(err))
				l.Close()
				return
			}
			log.Warn(common.NewError("reverse rebinding failed, retrying").Base(err))
			delay *= 2
			if delay > l.options.retry.Max {
				delay = l.options.retry.Max
			}
		}
	}
}

// acceptLoop pumps streams accepted from one session into the listener channel
func (l *ReverseListener) acceptLoop(rs *reverseSession) {
	for {
		stream, err := rs.session.AcceptStream()
		if err != nil {
			// the session is dead (socket error or explicit close): wake up
			// the maintain loop immediately
			rs.markDead()
			return
		}
		conn := &Conn{
			rwc:  stream,
			Conn: rs.underlay,
		}
		select {
		case l.connChan <- conn:
		case <-l.ctx.Done():
			stream.Close()
			return
		}
	}
}

// keepaliveLoop sends Ping frames on the control stream; drainControl reads
// the Pong frames so the control stream buffer never fills up. With
// keepalive-jitter configured the ping cadence is re-drawn per round so the
// probes don't form a fixed-period beacon.
func (l *ReverseListener) keepaliveLoop(rs *reverseSession) {
	go drainControl(rs)
	jitter := l.client.cfg.KeepaliveJitter
	var ticker *time.Ticker
	var timer *time.Timer
	var pingC <-chan time.Time
	if jitter > 0 {
		timer = time.NewTimer(jitterDuration(pingInterval(l.client.cfg.IdleTimeout), jitter))
		pingC = timer.C
	} else {
		ticker = time.NewTicker(pingInterval(l.client.cfg.IdleTimeout))
		pingC = ticker.C
	}
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-rs.dead:
			return
		case <-rs.session.CloseChan():
			rs.markDead()
			return
		case <-pingC:
			rs.control.SetWriteDeadline(time.Now().Add(controlWriteWait))
			err := writeFrame(rs.control, framePing, (&ping{Nonce: uint64(time.Now().UnixNano())}).marshal())
			if err != nil {
				rs.markDead()
				return
			}
			if timer != nil {
				timer.Reset(jitterDuration(pingInterval(l.client.cfg.IdleTimeout), jitter))
			}
		}
	}
}

// jitterDuration draws d*(1 ± jitter); 0/负截到 1ms 保底。
func jitterDuration(d time.Duration, jitter float64) time.Duration {
	delta := 2*rand.Float64() - 1
	out := time.Duration(float64(d) * (1 + jitter*delta))
	if out < time.Millisecond {
		out = time.Millisecond
	}
	return out
}

// drainControl consumes the Pong frames of one session; a control read error
// also means the session is gone
func drainControl(rs *reverseSession) {
	for {
		frameType, _, err := readFrame(rs.control)
		if err != nil {
			rs.markDead()
			return
		}
		if frameType != framePong {
			log.Debug("unexpected reverse control frame type", frameType)
		}
	}
}

// applyKeepalive scales the smux keepalive with the configured idle timeout so
// a small timeout also means fast dead-session detection
func applyKeepalive(smuxConfig *smux.Config, idleSeconds int) {
	if idleSeconds <= 0 {
		return
	}
	interval := time.Duration(idleSeconds) * time.Second / 3
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	if interval < time.Second {
		interval = time.Second
	}
	smuxConfig.KeepAliveInterval = interval
	smuxConfig.KeepAliveTimeout = interval * 3
}

func pingInterval(idleSeconds int) time.Duration {
	interval := idleTimeoutDuration(idleSeconds) / 3
	if interval < time.Second {
		interval = time.Second
	}
	return interval
}

func idleTimeoutDuration(idleSeconds int) time.Duration {
	if idleSeconds <= 0 {
		return 0
	}
	return time.Duration(idleSeconds) * time.Second
}
