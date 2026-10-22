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

// Client is a tls client
type Client struct {
	verify        bool
	sni           string
	ca            *x509.CertPool
	cipher        []uint16
	alpn          []string
	sessionTicket bool
	reuseSession  bool
	fingerprint   string
	certPin       string
	helloID       utls.ClientHelloID
	keyLogger     io.WriteCloser
	underlay      tunnel.Client
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
		tlsConn := utls.UClient(conn, &utls.Config{
			RootCAs:            c.ca,
			ServerName:         c.sni,
			InsecureSkipVerify: !c.verify,
			KeyLogWriter:       c.keyLogger,
			// a real browser hello always carries ALPN; omitting it while
			// impersonating one is a fingerprint contradiction of its own
			NextProtos: c.alpn,
		}, c.helloID)
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
	tlsConn := tls.Client(conn, &tls.Config{
		InsecureSkipVerify:     !c.verify,
		ServerName:             c.sni,
		RootCAs:                c.ca,
		KeyLogWriter:           c.keyLogger,
		CipherSuites:           c.cipher,
		NextProtos:             c.alpn,
		SessionTicketsDisabled: !c.sessionTicket,
	})
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
		underlay:      underlay,
		verify:        cfg.TLS.Verify,
		sni:           cfg.TLS.SNI,
		cipher:        fingerprint.ParseCipher(strings.Split(cfg.TLS.Cipher, ":")),
		alpn:          alpn,
		sessionTicket: cfg.TLS.ReuseSession,
		fingerprint:   cfg.TLS.Fingerprint,
		certPin:       normalizeCertPin(cfg.TLS.CertPinSHA256),
		helloID:       helloID,
	}

	if cfg.TLS.CertPath != "" {
		caCertByte, err := os.ReadFile(cfg.TLS.CertPath)
		if err != nil {
			return nil, common.NewError("failed to load cert file").Base(err)
		}
		client.ca = x509.NewCertPool()
		ok := client.ca.AppendCertsFromPEM(caCertByte)
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
