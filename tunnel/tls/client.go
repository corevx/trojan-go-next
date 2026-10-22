package tls

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"

	utls "github.com/refraction-networking/utls"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/tls/fingerprint"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

// sessionCacheSize 客户端会话缓存容量：256 足够覆盖常见并发连接规模，
// LRU 淘汰保证内存有上界。
const sessionCacheSize = 256

// Client is a tls client
type Client struct {
	fingerprint string
	certPin     string
	helloID     utls.ClientHelloID
	keyLogger   io.WriteCloser
	underlay    tunnel.Client

	// tlsConfig / utlsConfig 在 NewClient 构建一次、所有 DialConn 共享：
	// ClientSessionCache 必须挂在跨连接共享的 config 上，会话复用才可能发生
	// （每次拨号新建 config 等于给每个握手一个空缓存，票据永远用不出去）。
	// crypto/tls 与 utls 的 Config 按约定构建后不再修改即可并发使用。
	// 同一时刻只有一条路径非 nil：fingerprint 非空走 utlsConfig，否则 tlsConfig。
	tlsConfig  *tls.Config
	utlsConfig *utls.Config
}

func (c *Client) Close() error {
	if c.keyLogger != nil {
		c.keyLogger.Close()
	}
	return c.underlay.Close()
}

func (c *Client) DialPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	panic("not supported")
}

func (c *Client) DialConn(_ *tunnel.Address, overlay tunnel.Tunnel) (tunnel.Conn, error) {
	conn, err := c.underlay.DialConn(nil, &Tunnel{})
	if err != nil {
		return nil, common.NewError("tls failed to dial conn").Base(err)
	}

	if c.fingerprint != "" {
		// utls fingerprint
		tlsConn := utls.UClient(conn, c.utlsConfig, c.helloID)
		if err := tlsConn.Handshake(); err != nil {
			return nil, common.NewError("tls failed to handshake with remote server").Base(err)
		}
		if c.certPin != "" {
			if err := verifyCertPin(tlsConn.ConnectionState().PeerCertificates, c.certPin); err != nil {
				_ = conn.Close()
				return nil, common.NewError("tls certificate pin verification failed").Base(err)
			}
		}
		return &transport.Conn{
			Conn: tlsConn,
		}, nil
	}
	// golang default tls library
	tlsConn := tls.Client(conn, c.tlsConfig)
	err = tlsConn.Handshake()
	if err != nil {
		return nil, common.NewError("tls failed to handshake with remote server").Base(err)
	}
	if c.certPin != "" {
		if err := verifyCertPin(tlsConn.ConnectionState().PeerCertificates, c.certPin); err != nil {
			_ = conn.Close()
			return nil, common.NewError("tls certificate pin verification failed").Base(err)
		}
	}
	return &transport.Conn{
		Conn: tlsConn,
	}, nil
}

// NewClient creates a tls client
func NewClient(ctx context.Context, underlay tunnel.Client) (*Client, error) {
	cfg := config.FromContext(ctx, Name).(*Config)

	helloID := utls.ClientHelloID{}
	if cfg.TLS.Fingerprint != "" {
		switch cfg.TLS.Fingerprint {
		case "firefox":
			helloID = utls.HelloFirefox_Auto
		case "chrome":
			helloID = utls.HelloChrome_Auto
		case "ios":
			helloID = utls.HelloIOS_Auto
		default:
			return nil, common.NewError("invalid fingerprint " + cfg.TLS.Fingerprint)
		}
		log.Info("tls fingerprint", cfg.TLS.Fingerprint, "applied")
	}

	if cfg.TLS.SNI == "" {
		cfg.TLS.SNI = cfg.RemoteHost
		log.Warn("tls sni is unspecified")
	}

	alpn := cfg.TLS.ALPN
	if len(alpn) == 0 {
		// websocket needs HTTP/1.1; offering h2 risks negotiating it and
		// breaking the ws handshake
		alpn = []string{"http/1.1"}
	}

	client := &Client{
		underlay:    underlay,
		fingerprint: cfg.TLS.Fingerprint,
		certPin:     normalizeCertPin(cfg.TLS.CertPinSHA256),
		helloID:     helloID,
	}

	var ca *x509.CertPool
	if cfg.TLS.CertPath != "" {
		caCertByte, err := os.ReadFile(cfg.TLS.CertPath)
		if err != nil {
			return nil, common.NewError("failed to load cert file").Base(err)
		}
		ca = x509.NewCertPool()
		ok := ca.AppendCertsFromPEM(caCertByte)
		if !ok {
			log.Warn("invalid cert list")
		}
		log.Info("using custom cert")

		// print cert info
		pemCerts := caCertByte
		for len(pemCerts) > 0 {
			var block *pem.Block
			block, pemCerts = pem.Decode(pemCerts)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
				continue
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				continue
			}
			log.Trace("issuer:", cert.Issuer, "subject:", cert.Subject)
		}
	}

	if cfg.TLS.CertPath == "" {
		log.Info("cert is unspecified, using default ca list")
	}

	// 共享 config 就在此处定稿：reuse_session=true 时挂会话缓存，客户端跨
	// 连接复用 TLS 会话（缩写握手/PSK）；false 则保持每次完整握手（缓存为
	// nil 即标准库默认的不复用行为）。SessionTicketsDisabled 是纯服务端
	// 开关，对客户端握手无效果，客户端复用语义完全由 ClientSessionCache
	// 决定，不再设置以免误导。
	if cfg.TLS.Fingerprint != "" {
		utlsCfg := &utls.Config{
			RootCAs:            ca,
			ServerName:         cfg.TLS.SNI,
			InsecureSkipVerify: !cfg.TLS.Verify,
			KeyLogWriter:       client.keyLogger,
			// a real browser hello always carries ALPN; omitting it while
			// impersonating one is a fingerprint contradiction of its own
			NextProtos: alpn,
		}
		if cfg.TLS.ReuseSession {
			utlsCfg.ClientSessionCache = utls.NewLRUClientSessionCache(sessionCacheSize)
		}
		client.utlsConfig = utlsCfg
	} else {
		stdCfg := &tls.Config{
			InsecureSkipVerify: !cfg.TLS.Verify,
			ServerName:         cfg.TLS.SNI,
			RootCAs:            ca,
			KeyLogWriter:       client.keyLogger,
			CipherSuites:       fingerprint.ParseCipher(strings.Split(cfg.TLS.Cipher, ":")),
			NextProtos:         alpn,
		}
		if cfg.TLS.ReuseSession {
			stdCfg.ClientSessionCache = tls.NewLRUClientSessionCache(sessionCacheSize)
		}
		client.tlsConfig = stdCfg
	}

	log.Debug("tls client created")
	return client, nil
}

// normalizeCertPin 归一化配置写入的证书指纹：去冒号、转小写。接受
// "aa:bb:..." 冒号分组写法与裸 hex 两种形态；空串原样返回（未启用 pin）。
func normalizeCertPin(pin string) string {
	pin = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(pin, ":", "")))
	if pin == "" {
		return ""
	}
	log.Info("tls certificate pin enabled")
	return pin
}

// verifyCertPin 校验服务端证书链叶子证书的 SHA-256 指纹与钉死值一致。
// 场景：自签证书（Verify=false 无法走链校验）下钉死身份防 MITM——比裸关
// 校验安全，比装 CA 简单。certs 为空视为失败（匿名/异常握手）。
func verifyCertPin(certs []*x509.Certificate, pin string) error {
	if len(certs) == 0 {
		return common.NewError("no peer certificate presented to pin against")
	}
	sum := sha256.Sum256(certs[0].Raw)
	got := hex.EncodeToString(sum[:])
	if got != pin {
		return fmt.Errorf("peer certificate sha256 %s does not match pinned %s", got, pin)
	}
	log.Info("tls certificate pin verified")
	return nil
}
