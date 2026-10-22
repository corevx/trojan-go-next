//go:build linux
// +build linux

package nat

import (
	"context"

	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/proxy"
	"github.com/p4gefau1t/trojan-go/proxy/client"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/tproxy"
)

const Name = "NAT"

func init() {
	proxy.RegisterProxyCreator(Name, func(ctx context.Context) (*proxy.Proxy, error) {
		cfg := config.FromContext(ctx, Name).(*client.Config)
		ctx, cancel := context.WithCancel(ctx)
		serverStack := []string{tproxy.Name}
		// router 解禁（原「router is not allowed in nat mode」为上游历史
		// 遗留限制，非技术约束）：NAT(tproxy) 部署在 PREROUTING 网关形态下
		// 出站流量经网关自身路由转发，不存在 client 模式「router 判定直连
		// → 直连绕过代理 → 形成回环」的风险路径；且 router 栈按 GEO/IP
		// 规则对每个出站目标独立裁决，与入站形态正交。
		// 已知限制：PREROUTING 拿到的是重建后的 TCP 流，目标为 IP 而非
		// 域名——除非网关同时劫持 DNS（如 dnsmasq/iptables REDIRECT 指向
		// 本机），基于域名的分流规则在纯 IP 目标上无法命中，会落到
		// fallback 策略；IP/CIDR/GEOIP 规则不受影响。
		// router.enabled=false（默认）时栈组合与原先硬编码 false 完全一致。
		clientStack := client.GenerateClientTree(client.StackOptions{
			TransportPlugin: cfg.TransportPlugin.Enabled,
			Mux:             cfg.Mux.Enabled,
			Websocket:       cfg.Websocket.Enabled,
			Shadowsocks:     cfg.Shadowsocks.Enabled,
			Router:          cfg.Router.Enabled,
		})
		c, err := proxy.CreateClientStack(ctx, clientStack)
		if err != nil {
			cancel()
			return nil, err
		}
		s, err := proxy.CreateServerStack(ctx, serverStack)
		if err != nil {
			cancel()
			return nil, err
		}
		return proxy.NewProxy(ctx, cancel, []tunnel.Server{s}, c), nil
	})
}

func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return new(client.Config)
	})
}
