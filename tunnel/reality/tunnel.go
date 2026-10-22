// Package reality 实现客户端侧 REALITY 传输安全层：以 utls 指纹 ClientHello
// 伪装成访问真站的普通浏览器流量，将认证凭据封装进 ClientHello 的
// legacy_session_id 字段（x25519 ECDH + HKDF-SHA256 派生密钥的 AES-256-GCM
// 密封），并通过校验服务端临时 ed25519 证书签名域的 HMAC-SHA512 回执完成
// 对端认证。协议事实参考 Xray-core transport/internet/reality（MPL-2.0）、
// sing-box common/tls/reality_client.go（GPL-3.0）与 metacubex/utls 的
// RealityServer（GPL-3.0）的实现行为，本包为自研实现。
package reality

import (
	"context"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

const Name = "REALITY"

type Tunnel struct{}

func (t *Tunnel) Name() string {
	return Name
}

func (t *Tunnel) NewClient(ctx context.Context, client tunnel.Client) (tunnel.Client, error) {
	return NewClient(ctx, client)
}

func (t *Tunnel) NewServer(context.Context, tunnel.Server) (tunnel.Server, error) {
	return nil, common.NewError("reality server side is not implemented")
}

func init() {
	tunnel.RegisterTunnel(Name, &Tunnel{})
}
