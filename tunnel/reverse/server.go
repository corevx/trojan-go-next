package reverse

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/xtaci/smux"
)

// defaultMaxListenerConns is the per-listener concurrency cap (constraint #6)
const defaultMaxListenerConns = 128

// userHashConn is implemented by authenticated inbound connections (trojan's
// InboundConn), letting the reverse server learn the caller identity without
// importing the trojan package
type userHashConn interface {
	UserHash() string
}

// Server accepts reverse tunnel requests carried over authenticated trojan
// connections (trojan command REVERSE), binds ports and relays the inbound
// traffic of every bound port back over smux streams.
//
// Security model:
//  1. ACL: the caller must be a configured reverse user (SHA224 password hash
//     match), the port must be inside allow-ports, and privileged ports (<1024)
//     are always refused. Every check happens before net.Listen.
//  2. Port ownership is exclusive across all identities: a port already held by
//     any session is refused (code 2).
//  3. When a session dies, every listener and stream of that identity is closed
//     via context cancellation, so no orphaned ports or streams can survive.
//  4. The reverse protocol only ever rides inside an already-authenticated
//     TLS+trojan stream; there is no standalone listener to attack.
//  5. bind/unbind/listener-accept are logged with time, hash prefix, port and
//     the visitor IP.
//  6. Every listener has a concurrency cap (fail fast, no queueing) and every
//     relayed stream has an idle timeout.
//  7. A valid trojan user outside reverse.users is refused (code 1) here,
//     without affecting its normal proxy usage.
type Server struct {
	underlay tunnel.Server
	cfg      *Config
	users    map[string]*userACL

	mu        sync.Mutex
	portOwner map[int]*sessionEntry
	// hashListeners counts the bound ports per user identity (hash), to
	// enforce max-listeners across all sessions of one user
	hashListeners map[string]int
	sessions      map[*sessionEntry]struct{}
	ctx           context.Context
	cancel        context.CancelFunc
}

func (s *Server) AcceptConn(tunnel.Tunnel) (tunnel.Conn, error) {
	return nil, common.NewError("reverse server does not support AcceptConn")
}

func (s *Server) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	panic("not supported")
}

// Close tears down every reverse session. The underlay (the trojan server)
// belongs to the proxy node tree and is not closed here.
func (s *Server) Close() error {
	s.cancel()
	for _, entry := range s.collectSessions() {
		entry.close()
	}
	return nil
}

// CloseAllSessions tears down every live reverse session (admin/testing helper).
// Returning 0 means no session is currently established.
func (s *Server) CloseAllSessions() int {
	entries := s.collectSessions()
	for _, entry := range entries {
		entry.close()
	}
	return len(entries)
}

func (s *Server) collectSessions() []*sessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]*sessionEntry, 0, len(s.sessions))
	for entry := range s.sessions {
		entries = append(entries, entry)
	}
	return entries
}

func (s *Server) addSession(entry *sessionEntry) {
	s.mu.Lock()
	s.sessions[entry] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) removeSession(entry *sessionEntry) {
	s.mu.Lock()
	delete(s.sessions, entry)
	s.mu.Unlock()
}

// ListenOn consumes reverse connections from an additional underlay server
// (e.g. the websocket-path trojan server) while sharing the same port
// ownership registry.
func (s *Server) ListenOn(underlay tunnel.Server) {
	go s.acceptLoop(underlay)
}

func (s *Server) acceptLoop(underlay tunnel.Server) {
	for {
		conn, err := underlay.AcceptConn(&Tunnel{})
		if err != nil {
			log.Error(common.NewError("reverse failed to accept conn").Base(err))
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn tunnel.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Error(common.NewError("panic in reverse handleConn").Base(fmt.Errorf("%v", r)))
		}
	}()
	defer conn.Close()

	// constraint #4: this connection has already passed trojan authentication
	hash := ""
	if identity, ok := conn.(userHashConn); ok {
		hash = identity.UserHash()
	}

	// the server side plays the smux *client* role: it opens one stream per
	// visitor connecting to a bound port, plus the control stream
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxStreamBuffer = s.cfg.StreamBuffer
	smuxConfig.MaxReceiveBuffer = s.cfg.StreamBuffer
	applyKeepalive(smuxConfig, s.cfg.IdleTimeout)
	session, err := smux.Client(conn, smuxConfig)
	if err != nil {
		log.Error(common.NewError("reverse failed to create smux session").Base(err))
		return
	}

	control, err := session.OpenStream()
	if err != nil {
		log.Error(common.NewError("reverse failed to open control stream").Base(err))
		session.Close()
		return
	}
	control.SetReadDeadline(time.Now().Add(bindTimeout))
	frameType, payload, err := readFrame(control)
	control.SetReadDeadline(time.Time{})
	if err != nil {
		log.Error(common.NewError("reverse failed to read bind request").Base(err))
		session.Close()
		return
	}
	if frameType != frameBindReq {
		log.Error(common.NewError("unexpected reverse frame on control stream"))
		s.replyBind(control, &bindResp{Code: CodeBadParam, Msg: "expected bind request"})
		session.Close()
		return
	}
	req, err := parseBindReq(payload)
	if err != nil {
		s.replyBind(control, &bindResp{Code: CodeBadParam, Msg: "malformed bind request"})
		session.Close()
		return
	}

	entry := newSessionEntry(s, conn, session, control, hash)
	s.addSession(entry)
	defer entry.close()

	resp := s.bind(entry, req.Port)
	s.replyBind(control, resp)
	if resp.Code != CodeOK {
		return
	}
	log.WithFields(log.Fields{
		"hash":   hashPrefix(entry.hash),
		"port":   req.Port,
		"remote": conn.RemoteAddr().String(),
	}).Info("reverse port bound")

	go entry.controlLoop()
	<-entry.ctx.Done()
}

func (s *Server) replyBind(control *smux.Stream, resp *bindResp) {
	control.SetWriteDeadline(time.Now().Add(controlWriteWait))
	err := writeFrame(control, frameBindResp, resp.marshal())
	control.SetWriteDeadline(time.Time{})
	if err != nil {
		log.Error(common.NewError("reverse failed to send bind response").Base(err))
	}
}

// bind enforces the ACL (constraint #1) and the port ownership (constraint #2)
// before any net.Listen, then registers the listener on the entry
func (s *Server) bind(entry *sessionEntry, port int) *bindResp {
	// constraint #7: a trojan user not listed in reverse users is refused
	acl, found := s.users[entry.hash]
	if !found {
		return &bindResp{Code: CodeACL, Msg: "user is not allowed to open reverse tunnels"}
	}
	if port < 1 || port > 65535 {
		return &bindResp{Code: CodeBadParam, Msg: "invalid port"}
	}
	if !acl.allows(port) {
		return &bindResp{Code: CodeACL, Msg: "port " + strconv.Itoa(port) + " is not allowed"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hashListeners[entry.hash] >= acl.maxListener {
		return &bindResp{Code: CodeLimit, Msg: "listener limit reached"}
	}
	if _, owned := s.portOwner[port]; owned {
		return &bindResp{Code: CodeInUse, Msg: "port " + strconv.Itoa(port) + " is already bound"}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(acl.bindHost, strconv.Itoa(port)))
	if err != nil {
		return &bindResp{Code: CodeInUse, Msg: "failed to bind port " + strconv.Itoa(port) + ": " + err.Error()}
	}
	s.portOwner[port] = entry
	s.hashListeners[entry.hash]++
	entry.addListener(port, listener)
	return &bindResp{Code: CodeOK, Port: port}
}

// release drops the ownership of port if it is currently held by entry
func (s *Server) release(entry *sessionEntry, port int) {
	s.mu.Lock()
	if owner, ok := s.portOwner[port]; ok && owner == entry {
		delete(s.portOwner, port)
		s.decListener(entry.hash)
	}
	s.mu.Unlock()
}

func (s *Server) releaseAll(entry *sessionEntry) {
	s.mu.Lock()
	for port, owner := range s.portOwner {
		if owner == entry {
			delete(s.portOwner, port)
			s.decListener(entry.hash)
		}
	}
	s.mu.Unlock()
}

// decListener decrements the per-identity listener counter.
// Must be called while holding server.mu.
func (s *Server) decListener(hash string) {
	if n := s.hashListeners[hash]; n <= 1 {
		delete(s.hashListeners, hash)
	} else {
		s.hashListeners[hash] = n - 1
	}
}

// sessionEntry is one reverse session, i.e. one authenticated smux session
// holding zero or more bound ports
type sessionEntry struct {
	server  *Server
	conn    tunnel.Conn
	session *smux.Session
	control *smux.Stream
	hash    string
	ctx     context.Context
	cancel  context.CancelFunc

	mu        sync.Mutex
	listeners map[int]*boundListener
	closeOnce sync.Once
}

func newSessionEntry(server *Server, conn tunnel.Conn, session *smux.Session, control *smux.Stream, hash string) *sessionEntry {
	ctx, cancel := context.WithCancel(server.ctx)
	return &sessionEntry{
		server:    server,
		conn:      conn,
		session:   session,
		control:   control,
		hash:      hash,
		ctx:       ctx,
		cancel:    cancel,
		listeners: make(map[int]*boundListener),
	}
}

// addListener registers a bound port and starts its accept loop.
// Must be called while holding server.mu, before the bind response is sent.
func (e *sessionEntry) addListener(port int, listener net.Listener) {
	bl := &boundListener{
		entry:    e,
		port:     port,
		listener: listener,
		sem:      make(chan struct{}, defaultMaxListenerConns),
		closed:   make(chan struct{}),
	}
	e.mu.Lock()
	e.listeners[port] = bl
	e.mu.Unlock()
	go bl.acceptLoop()
}

// unbind releases a single port on request of the client (UnbindReq)
func (e *sessionEntry) unbind(port int) {
	e.mu.Lock()
	bl := e.listeners[port]
	delete(e.listeners, port)
	e.mu.Unlock()
	if bl == nil {
		return
	}
	bl.close()
	e.server.release(e, port)
	log.WithFields(log.Fields{
		"hash": hashPrefix(e.hash),
		"port": port,
	}).Info("reverse port unbound")
}

// close tears down the whole identity: every listener, every stream and the
// session itself (constraint #3), and releases all port ownership
func (e *sessionEntry) close() {
	e.closeOnce.Do(func() {
		e.cancel()
		e.mu.Lock()
		listeners := make([]*boundListener, 0, len(e.listeners))
		for _, bl := range e.listeners {
			listeners = append(listeners, bl)
		}
		e.listeners = make(map[int]*boundListener)
		e.mu.Unlock()
		for _, bl := range listeners {
			bl.close()
		}
		e.server.releaseAll(e)
		e.server.removeSession(e)
		e.session.Close()
		e.control.Close()
		e.conn.Close()
		if len(listeners) > 0 {
			log.WithFields(log.Fields{
				"hash":      hashPrefix(e.hash),
				"listeners": len(listeners),
			}).Info("reverse session closed, listeners released")
		}
	})
}

// controlLoop serves Ping/Pong and UnbindReq on the control stream; a read
// error means the session is gone and cascades the whole entry teardown
func (e *sessionEntry) controlLoop() {
	for {
		frameType, payload, err := readFrame(e.control)
		if err != nil {
			e.close()
			return
		}
		switch frameType {
		case framePing:
			req, err := parsePing(payload)
			if err != nil {
				log.Debug("malformed reverse ping:", err)
				continue
			}
			e.control.SetWriteDeadline(time.Now().Add(controlWriteWait))
			if err := writeFrame(e.control, framePong, (&pong{Nonce: req.Nonce}).marshal()); err != nil {
				e.close()
				return
			}
		case frameUnbindReq:
			req, err := parseUnbindReq(payload)
			if err != nil {
				log.Debug("malformed reverse unbind request:", err)
				continue
			}
			e.unbind(req.Port)
		default:
			log.Debug("unexpected reverse control frame type", frameType)
		}
	}
}

// boundListener is one port bound on the server on behalf of a session
type boundListener struct {
	entry    *sessionEntry
	port     int
	listener net.Listener
	sem      chan struct{}
	closed   chan struct{}

	closeOnce sync.Once
}

func (b *boundListener) close() {
	b.closeOnce.Do(func() {
		close(b.closed)
		b.listener.Close()
	})
}

func (b *boundListener) acceptLoop() {
	defer b.listener.Close()
	idleTimeout := idleTimeoutDuration(b.entry.server.cfg.IdleTimeout)
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			select {
			case <-b.closed:
				return
			case <-b.entry.ctx.Done():
				return
			default:
				log.Error(common.NewError("reverse listener accept failed").Base(err))
				time.Sleep(time.Millisecond * 100)
				continue
			}
		}
		// constraint #6: per-listener concurrency cap, fail fast without queueing
		select {
		case b.sem <- struct{}{}:
		default:
			log.WithFields(log.Fields{
				"hash": hashPrefix(b.entry.hash),
				"port": b.port,
			}).Warn("reverse listener concurrency limit reached, dropping connection")
			conn.Close()
			continue
		}
		// constraint #5: log the listener accept
		log.WithFields(log.Fields{
			"hash":   hashPrefix(b.entry.hash),
			"port":   b.port,
			"remote": conn.RemoteAddr().String(),
		}).Debug("reverse connection accepted")
		stream, err := b.entry.session.OpenStream()
		if err != nil {
			// the session is dead: drop the visitor conn instead of requeueing
			// it on the hot path
			conn.Close()
			<-b.sem
			b.close()
			return
		}
		go func(conn net.Conn, stream *smux.Stream) {
			defer func() { <-b.sem }()
			defer conn.Close()
			defer stream.Close()
			var streamConn net.Conn = stream
			if idleTimeout > 0 {
				streamConn = &idleTimeoutConn{Conn: stream, timeout: idleTimeout}
			}
			relayConn(b.entry.ctx, conn, streamConn)
		}(conn, stream)
	}
}

func hashPrefix(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func NewServer(ctx context.Context, underlay tunnel.Server) (*Server, error) {
	cfgInterface := config.FromContext(ctx, Name)
	if cfgInterface == nil {
		return nil, common.NewError("reverse config not found")
	}
	cfg := cfgInterface.(*Config)

	users := make(map[string]*userACL)
	for _, u := range cfg.Users {
		portRanges, err := parsePortRanges(u.AllowPorts)
		if err != nil {
			return nil, common.NewError("reverse failed to parse allow-ports").Base(err)
		}
		maxListeners := u.MaxListeners
		if maxListeners <= 0 {
			maxListeners = defaultMaxListeners
		}
		bindHost := u.BindHost
		if bindHost == "" {
			bindHost = cfg.DefaultBindHost
		}
		if bindHost == "" {
			bindHost = defaultBindHost
		}
		users[common.SHA224String(u.Password)] = &userACL{
			bindHost:    bindHost,
			maxListener: maxListeners,
			allowPorts:  portRanges,
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	server := &Server{
		underlay:      underlay,
		cfg:           cfg,
		users:         users,
		portOwner:     make(map[int]*sessionEntry),
		hashListeners: make(map[string]int),
		sessions:      make(map[*sessionEntry]struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}
	go server.acceptLoop(underlay)
	log.Debug("reverse server created")
	return server, nil
}
