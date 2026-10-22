package shadowtls

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

// handshakeTimeout 限制单条入站连接在 shadowtls 握手阶段（读 ClientHello、
// 中继真站握手、等待 auth 帧）的总停留时长，防静默连接卡死 per-conn
// goroutine；进入数据期或回退中继前清除。
var handshakeTimeout = 30 * time.Second

// destDialTimeout 真站 TCP 连接超时
const destDialTimeout = 10 * time.Second

type user struct {
	Name     string
	Password string
}

// Server 是 shadowtls 隧道层服务端：消费传输层裸 TCP 连接，完成 v3 握手
// 中继与验签后，把内层（裸 trojan）连接交给上层。
type Server struct {
	underlay tunnel.Server
	users    []user
	dest     string
	strict   bool
	wildcard int
	dialer   *net.Dialer
	connChan chan tunnel.Conn
	ctx      context.Context
	cancel   context.CancelFunc
}

func (s *Server) Close() error {
	s.cancel()
	return s.underlay.Close()
}

// Conn 实现 tunnel.Conn：shadowtls 层不解析内层协议，元数据由 trojan 层
// 自行填充（与 transport.Conn 一致）
type Conn struct {
	net.Conn
}

func (c *Conn) Metadata() *tunnel.Metadata {
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.underlay.AcceptConn(&Tunnel{})
		if err != nil {
			select {
			case <-s.ctx.Done():
			default:
				log.Error(common.NewError("shadowtls failed to accept conn").Base(err))
			}
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	// handled 表示连接生命周期已收尾（回退/降级中继跑完）或所有权已移交
	// 上层；此前任何退出路径都要关闭连接
	handled := false
	defer func() {
		if r := recover(); r != nil {
			log.Error(common.NewError("panic in shadowtls handleConn goroutine").Base(fmt.Errorf("%v", r)))
		}
		if !handled {
			conn.Close()
		}
	}()

	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	clientHello, err := readFrame(conn)
	if err != nil {
		return
	}
	serverName, sniErr := extractServerName(clientHello)
	if sniErr != nil {
		log.Debug("shadowtls failed to extract SNI:", sniErr)
	}

	u, authErr := verifyClientHello(clientHello, s.users)
	if authErr != nil {
		// 验签失败：连接整体转为到真站的纯 TCP 中继（对外与直连真站无差别，
		// 规格§4.9），服务端不主动断连、不回 alert
		log.Warn("shadowtls client hello verify failed from", conn.RemoteAddr())
		s.relayFallback(conn, clientHello, serverName, false)
		return
	}
	log.Info("shadowtls user", u.Name, "authenticated from", conn.RemoteAddr())

	destConn, err := s.dialDest(serverName, true)
	if err != nil {
		log.Error(common.NewError("shadowtls failed to dial dest").Base(err))
		return
	}
	defer destConn.Close()
	if _, err = destConn.Write(clientHello); err != nil {
		log.Error(common.NewError("shadowtls failed to forward client hello").Base(err))
		return
	}
	// 只读一帧即提取 ServerRandom：天然绑定第一个 ServerHello（HRR 场景
	// 下三方客户端均绑第一帧，规格 §4.8）
	serverHello, err := readFrame(destConn)
	if err != nil {
		log.Error(common.NewError("shadowtls failed to read server hello").Base(err))
		return
	}
	if _, err = conn.Write(serverHello); err != nil {
		log.Error(common.NewError("shadowtls failed to forward server hello").Base(err))
		return
	}

	serverRandom := extractServerRandom(serverHello)
	if serverRandom == nil {
		log.Warn("shadowtls server random extract failed, will copy bidirectional")
		s.relayPlain(conn, destConn)
		return
	}
	if s.strict && !isServerHelloTLS13(serverHello) {
		log.Warn("shadowtls: TLS 1.3 is not supported by dest, will copy bidirectional")
		s.relayPlain(conn, destConn)
		return
	}

	password := u.Password
	hmacR := hmac.New(sha1.New, []byte(password))
	_, _ = hmacR.Write(serverRandom)
	hmacS := hmac.New(sha1.New, []byte(password))
	_, _ = hmacS.Write(serverRandom)
	_, _ = hmacS.Write([]byte("S"))
	hmacC := hmac.New(sha1.New, []byte(password))
	resetC := func() { hmacReset(hmacC, serverRandom, 'C') }
	resetC()

	request, err := relayHandshakeV3(conn, destConn, password, serverRandom, hmacR, hmacC, resetC)
	if err != nil {
		log.Error(common.NewError("shadowtls handshake relay failed").Base(err))
		return
	}
	// 协议层接管，deadline 交由上层/连接自身管理
	conn.SetDeadline(time.Time{})

	inner := &Conn{Conn: &prefixConn{Conn: newVerifiedConn(conn, hmacS, hmacC), prefix: request}}
	select {
	case s.connChan <- inner:
		// 所有权移交给上层（trojan），本 goroutine 退出时不得关闭连接
		handled = true
	case <-s.ctx.Done():
		log.Debug("shadowtls server closing, discarding conn")
	}
}

// relayFallback 验签失败路径：把已读的前缀字节垫回，与按 SNI 选择的真站
// 做纯双向中继直到连接结束（规格 §4.9）
func (s *Server) relayFallback(conn net.Conn, prefix []byte, serverName string, authenticated bool) {
	destConn, err := s.dialDest(serverName, authenticated)
	if err != nil {
		log.Error(common.NewError("shadowtls failed to dial fallback dest").Base(err))
		return
	}
	defer destConn.Close()
	cached := &prefixConn{Conn: conn, prefix: prefix}
	conn.SetDeadline(time.Time{})
	s.relayPlain(cached, destConn)
}

// relayPlain 纯双向中继：某方向干净 EOF 时对下游半关闭（CloseWrite，
// 让对向仍有时间送达剩余数据）；出错才双向关闭。两条方向均结束后返回。
func (s *Server) relayPlain(a, b net.Conn) {
	errChan := make(chan error, 2)
	copyDir := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		if err == nil {
			if closer, ok := dst.(interface{ CloseWrite() error }); ok {
				_ = closer.CloseWrite()
			} else {
				_ = dst.Close()
			}
		} else {
			_ = dst.Close()
			_ = src.Close()
		}
		errChan <- err
	}
	go copyDir(a, b)
	go copyDir(b, a)
	<-errChan
	<-errChan
}

// dialDest 按 wildcard SNI 策略与 SNI 选择真站目标。三态语义见规格 §4.9：
// off=恒用配置真站；authed=验签通过且 SNI 无映射时用 SNI:443 自身当真站；
// all=一律优先 SNI:443。本实现不支持 per-SNI 映射表，authed/all 模式下
// SNI 即目标。
func (s *Server) dialDest(serverName string, authenticated bool) (net.Conn, error) {
	dest := s.dest
	if serverName != "" {
		switch s.wildcard {
		case wildcardSNIAll:
			dest = net.JoinHostPort(serverName, "443")
		case wildcardSNIAuthed:
			if authenticated {
				dest = net.JoinHostPort(serverName, "443")
			}
		}
	}
	return s.dialer.Dial("tcp", dest)
}

func (s *Server) AcceptConn(tunnel.Tunnel) (tunnel.Conn, error) {
	select {
	case conn := <-s.connChan:
		return conn, nil
	case <-s.ctx.Done():
		return nil, common.NewError("shadowtls server closed")
	}
}

func (s *Server) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	panic("not supported")
}

const (
	wildcardSNIOff    = iota // 默认：恒用配置真站
	wildcardSNIAuthed        // 验签通过后用 SNI 自身当真站
	wildcardSNIAll           // 一律用 SNI 自身当真站
)

// NewServer 创建 shadowtls 层服务端。栈组合约束：本层之下是 transport，
// 之上是裸 trojan（无 tls/websocket 层）——外层 TLS 由真站握手中继提供。
func NewServer(ctx context.Context, underlay tunnel.Server) (*Server, error) {
	cfg, _ := config.FromContext(ctx, Name).(*Config)
	if cfg == nil {
		return nil, common.NewError("shadowtls config missing")
	}
	if cfg.ShadowTLS.Version == 0 {
		log.Warn("shadowtls version unset, defaulting to 3")
		cfg.ShadowTLS.Version = 3
	}
	if cfg.ShadowTLS.Version != 3 {
		return nil, common.NewError("unsupported shadowtls version " + strconv.Itoa(cfg.ShadowTLS.Version) + ": this implementation only supports v3")
	}
	if len(cfg.ShadowTLS.Users) == 0 {
		return nil, common.NewError("shadowtls v3 requires at least one user (users[])")
	}
	if cfg.ShadowTLS.Handshake.Server == "" || cfg.ShadowTLS.Handshake.ServerPort == 0 {
		return nil, common.NewError("shadowtls requires handshake.server / handshake.server-port (the real TLS site to relay)")
	}
	wildcard, err := parseWildcardSNI(cfg.ShadowTLS.WildcardSNI)
	if err != nil {
		return nil, err
	}
	dest := net.JoinHostPort(cfg.ShadowTLS.Handshake.Server, strconv.Itoa(cfg.ShadowTLS.Handshake.ServerPort))

	ctx, cancel := context.WithCancel(ctx)
	server := &Server{
		underlay: underlay,
		users:    make([]user, 0, len(cfg.ShadowTLS.Users)),
		dest:     dest,
		strict:   cfg.ShadowTLS.Strict,
		wildcard: wildcard,
		dialer:   &net.Dialer{Timeout: destDialTimeout},
		connChan: make(chan tunnel.Conn, 32),
		ctx:      ctx,
		cancel:   cancel,
	}
	for _, u := range cfg.ShadowTLS.Users {
		if u.Name == "" || u.Password == "" {
			cancel()
			return nil, common.NewError("shadowtls user requires both name and password")
		}
		server.users = append(server.users, user{Name: u.Name, Password: u.Password})
	}

	// 启动期连通性预检：真站不可达通常意味着配置笔误，fail-fast
	probe, err := server.dialDest("", false)
	if err != nil {
		cancel()
		return nil, common.NewError("shadowtls handshake dest unreachable: " + dest).Base(err)
	}
	probe.Close()

	go server.acceptLoop()
	log.WithFields(log.Fields{
		"dest":     dest,
		"strict":   server.strict,
		"users":    len(server.users),
		"wildcard": cfg.ShadowTLS.WildcardSNI,
	}).Info("shadowtls v3 server created")
	return server, nil
}

func parseWildcardSNI(s string) (int, error) {
	switch s {
	case "", "off":
		return wildcardSNIOff, nil
	case "authed":
		return wildcardSNIAuthed, nil
	case "all":
		return wildcardSNIAll, nil
	default:
		return 0, common.NewError("invalid shadowtls wildcard-sni mode " + s + ": must be one of off/authed/all")
	}
}
