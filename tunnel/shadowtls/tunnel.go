package shadowtls

import (
	"context"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

const Name = "SHADOWTLS"

// Tunnel 注册 shadowtls 隧道层。当前仅实现 v3 服务端：客户端侧由主流
// 实现（mihomo / sing-box）承担，本层 Client 不提供。
type Tunnel struct{}

func (*Tunnel) Name() string {
	return Name
}

func (*Tunnel) NewClient(ctx context.Context, client tunnel.Client) (tunnel.Client, error) {
	return nil, common.NewError("shadowtls client is not implemented; connect with mihomo / sing-box v3 clients")
}

func (*Tunnel) NewServer(ctx context.Context, server tunnel.Server) (tunnel.Server, error) {
	return NewServer(ctx, server)
}

func init() {
	tunnel.RegisterTunnel(Name, &Tunnel{})
}
