package reality

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

// sessionIDOffset 是 legacy_session_id 字段在 ClientHello 握手消息（含 4
// 字节 handshake 头）中的固定偏移：4(头)+2(legacy_version)+32(random)+1(长度)。
const sessionIDOffset = 39

// 协议常量：HKDF info 串与 short_id 字节宽度。
const (
	hkdfInfo   = "REALITY"
	shortIDLen = 8
)

// clientVersion 填入 session_id 前 3 字节的客户端版本标记。服务端默认不
// 约束（sing-box/mihomo 不暴露 min/max_client_ver 配置），仅 Xray 服务端
// 可选开启版本门槛，此处取保守值。
var clientVersion = [3]byte{1, 0, 0}

// Client is a reality client
type Client struct {
	publicKey   *ecdh.PublicKey
	shortID     [shortIDLen]byte
	serverName  string
	fingerprint string
	helloID     utls.ClientHelloID
	underlay    tunnel.Client
}

func (c *Client) Close() error {
	return c.underlay.Close()
}

func (c *Client) DialPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	panic("not supported")
}

func (c *Client) DialConn(_ *tunnel.Address, overlay tunnel.Tunnel) (tunnel.Conn, error) {
	conn, err := c.underlay.DialConn(nil, &Tunnel{})
	if err != nil {
		return nil, common.NewError("reality failed to dial conn").Base(err)
	}
	uconn, err := c.handshake(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &transport.Conn{Conn: uconn}, nil
}

// verifier 完成 REALITY 的服务端真伪判定：认证通过的服务端会返回一张临时
// ed25519 自签证书，其 DER 末 64 字节签名域被覆写为 HMAC-SHA512(authKey,
// 公钥)；除此之外拿到的都是回落真站（或被劫持）的真实证书。
type verifier struct {
	serverName string
	authKey    []byte
	verified   bool
}

func (v *verifier) VerifyPeerCertificate(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return common.NewError("reality: no peer certificate presented")
	}
	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, der := range rawCerts {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return common.NewError("reality: failed to parse peer certificate").Base(err)
		}
		certs = append(certs, cert)
	}
	if pub, ok := certs[0].PublicKey.(ed25519.PublicKey); ok {
		h := hmac.New(sha512.New, v.authKey)
		h.Write(pub)
		if hmac.Equal(h.Sum(nil), certs[0].Signature) {
			v.verified = true
			return nil
		}
	}
	// 非 REALITY 回执：对端是回落真站或中间人。走标准链校验——对真站证书
	// 可能通过（此时 verified 仍为 false，由 handshake 判死），对自签证书
	// 直接失败。
	opts := x509.VerifyOptions{
		DNSName:       v.serverName,
		Intermediates: x509.NewCertPool(),
	}
	for _, cert := range certs[1:] {
		opts.Intermediates.AddCert(cert)
	}
	if _, err := certs[0].Verify(opts); err != nil {
		return common.NewError("reality: peer certificate is neither REALITY-signed nor verifiable").Base(err)
	}
	return nil
}

// filterX25519MLKEM768 从 ClientHello 中剔除 X25519MLKEM768：REALITY 服务端
// 首选纯 x25519 keyshare（混合组的 ECDH 份额取法依赖各端实现细节），与
// sing-box 客户端同款处理。
func filterX25519MLKEM768(uconn *utls.UConn) {
	for _, ext := range uconn.Extensions {
		switch e := ext.(type) {
		case *utls.SupportedCurvesExtension:
			curves := e.Curves[:0]
			for _, id := range e.Curves {
				if id != utls.X25519MLKEM768 {
					curves = append(curves, id)
				}
			}
			e.Curves = curves
		case *utls.KeyShareExtension:
			shares := e.KeyShares[:0]
			for _, ks := range e.KeyShares {
				if ks.Group != utls.X25519MLKEM768 {
					shares = append(shares, ks)
				}
			}
			e.KeyShares = shares
		}
	}
}

// handshake 在 underlay 连接上完成 REALITY 握手，返回已认证的 TLS 1.3 连接。
func (c *Client) handshake(conn net.Conn) (*utls.UConn, error) {
	v := &verifier{serverName: c.serverName}
	uConfig := &utls.Config{
		ServerName:             c.serverName,
		InsecureSkipVerify:     true, // 真伪判定走 HMAC 回执，不走链校验
		SessionTicketsDisabled: true, // 会话复用会改变 ClientHello 形状，与 REALITY 互斥
		VerifyPeerCertificate:  v.VerifyPeerCertificate,
	}
	uconn := utls.UClient(conn, uConfig, c.helloID)
	if err := uconn.BuildHandshakeState(); err != nil {
		return nil, common.NewError("reality failed to build client hello").Base(err)
	}
	filterX25519MLKEM768(uconn)
	if err := uconn.BuildHandshakeState(); err != nil {
		return nil, common.NewError("reality failed to rebuild client hello").Base(err)
	}

	hello := uconn.HandshakeState.Hello
	ecdheKey := uconn.HandshakeState.State13.KeyShareKeys.Ecdhe
	if ecdheKey == nil {
		return nil, common.NewError("reality fingerprint " + c.fingerprint + " has no x25519 key share")
	}

	// 先将 Raw 中的 session_id 区清零作为 AEAD 的 AAD 基线（服务端解密前
	// 同样将该区域清零），再把密封结果写回字段；Handshake 内部会按字段
	// 重排 ClientHello，字段值即最终上线字节。
	hello.SessionId = make([]byte, 32)
	copy(hello.Raw[sessionIDOffset:], hello.SessionId)
	authKey, sessionID, err := sealSessionID(hello.Random, hello.Raw, ecdheKey, c.publicKey, c.shortID, time.Now())
	if err != nil {
		return nil, err
	}
	v.authKey = authKey
	copy(hello.SessionId, sessionID)
	copy(hello.Raw[sessionIDOffset:], hello.SessionId)

	if err := uconn.Handshake(); err != nil {
		return nil, common.NewError("reality failed to handshake with remote server").Base(err)
	}
	if !v.verified {
		return nil, common.NewError("reality verification failed: server presented a real certificate (fallback or MITM)")
	}
	return uconn, nil
}

// sealSessionID 构造 REALITY 认证材料：以客户端 keyshare 私钥与服务端静态
// 公钥的 x25519 共享密钥派生 authKey（HKDF-SHA256，salt=hello.Random[:20]，
// info="REALITY"），再用 AES-256-GCM（nonce=Random[20:32]，AAD=session_id
// 区清零的原始 ClientHello）将 [版本|时间戳|short_id] 16 字节明文密封成
// 32 字节写满 session_id。
func sealSessionID(random, rawHello []byte, ecdheKey *ecdh.PrivateKey, serverPub *ecdh.PublicKey, shortID [shortIDLen]byte, now time.Time) (authKey, sessionID []byte, err error) {
	if len(random) != 32 {
		return nil, nil, common.NewError("reality hello random must be 32 bytes")
	}
	shared, err := ecdheKey.ECDH(serverPub)
	if err != nil {
		return nil, nil, common.NewError("reality failed to derive x25519 shared secret").Base(err)
	}
	authKey, err = hkdf.Key(sha256.New, shared, random[:20], hkdfInfo, 32)
	if err != nil {
		return nil, nil, common.NewError("reality failed to derive auth key").Base(err)
	}
	block, err := aes.NewCipher(authKey)
	if err != nil {
		return nil, nil, common.NewError("reality failed to create cipher").Base(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, common.NewError("reality failed to create AEAD").Base(err)
	}
	sessionID = make([]byte, 32)
	copy(sessionID[0:3], clientVersion[:])
	sessionID[3] = 0 // reserved
	binary.BigEndian.PutUint32(sessionID[4:], uint32(now.Unix()))
	copy(sessionID[8:], shortID[:])
	aead.Seal(sessionID[:0], random[20:], sessionID[:16], rawHello)
	return authKey, sessionID, nil
}

// NewClient creates a reality client
func NewClient(ctx context.Context, underlay tunnel.Client) (*Client, error) {
	cfg := config.FromContext(ctx, Name).(*Config)
	rcfg := cfg.Reality

	rawPub, err := base64.RawURLEncoding.DecodeString(rcfg.PublicKey)
	if err != nil {
		return nil, common.NewError("reality failed to decode public-key").Base(err)
	}
	if len(rawPub) != 32 {
		return nil, common.NewError("reality public-key must be 32 bytes")
	}
	publicKey, err := ecdh.X25519().NewPublicKey(rawPub)
	if err != nil {
		return nil, common.NewError("reality invalid public-key").Base(err)
	}

	rawShortID, err := hex.DecodeString(rcfg.ShortID)
	if err != nil {
		return nil, common.NewError("reality failed to decode short-id").Base(err)
	}
	if len(rawShortID) > shortIDLen {
		return nil, common.NewError("reality short-id must be at most 8 bytes")
	}
	var shortID [shortIDLen]byte
	copy(shortID[:], rawShortID)

	serverName := rcfg.ServerName
	if serverName == "" {
		serverName = cfg.RemoteHost
		log.Warn("reality server-name is unspecified, falling back to remote_addr")
	}

	var helloID utls.ClientHelloID
	switch rcfg.Fingerprint {
	case "firefox":
		helloID = utls.HelloFirefox_Auto
	case "chrome":
		helloID = utls.HelloChrome_Auto
	case "ios":
		helloID = utls.HelloIOS_Auto
	default:
		return nil, common.NewError("invalid fingerprint " + rcfg.Fingerprint)
	}

	log.Info("reality client created, server name:", serverName, "fingerprint:", rcfg.Fingerprint)
	return &Client{
		publicKey:   publicKey,
		shortID:     shortID,
		serverName:  serverName,
		fingerprint: rcfg.Fingerprint,
		helloID:     helloID,
		underlay:    underlay,
	}, nil
}
