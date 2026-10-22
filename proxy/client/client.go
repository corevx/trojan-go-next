package client

import (
	"context"

	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/proxy"
	"github.com/p4gefau1t/trojan-go/tunnel/adapter"
	"github.com/p4gefau1t/trojan-go/tunnel/http"
	"github.com/p4gefau1t/trojan-go/tunnel/mux"
	"github.com/p4gefau1t/trojan-go/tunnel/router"
	"github.com/p4gefau1t/trojan-go/tunnel/shadowsocks"
	"github.com/p4gefau1t/trojan-go/tunnel/simplesocks"
	"github.com/p4gefau1t/trojan-go/tunnel/socks"
	"github.com/p4gefau1t/trojan-go/tunnel/tls"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
	"github.com/p4gefau1t/trojan-go/tunnel/trojan"
	"github.com/p4gefau1t/trojan-go/tunnel/websocket"
)

const Name = "CLIENT"

// StackOptions 描述出站协议栈各可选层的启用组合，替代原先五个
// 位置参数的 bool 串（调用点可读性差且易错位）。
type StackOptions struct {
	TransportPlugin bool
	Mux             bool
	Websocket       bool
	Shadowsocks     bool
	Router          bool
}

// GenerateClientTree generate general outbound protocol stack
func GenerateClientTree(opts StackOptions) []string {
	clientStack := []string{transport.Name}
	if !opts.TransportPlugin {
		clientStack = append(clientStack, tls.Name)
	}
	if opts.Websocket {
		clientStack = append(clientStack, websocket.Name)
	}
	if opts.Shadowsocks {
		clientStack = append(clientStack, shadowsocks.Name)
	}
	clientStack = append(clientStack, trojan.Name)
	if opts.Mux {
		clientStack = append(clientStack, []string{mux.Name, simplesocks.Name}...)
	}
	if opts.Router {
		clientStack = append(clientStack, router.Name)
	}
	return clientStack
}

func init() {
	proxy.RegisterProxyCreator(Name, func(ctx context.Context) (*proxy.Proxy, error) {
		cfg := config.FromContext(ctx, Name).(*Config)
		adapterServer, err := adapter.NewServer(ctx, nil)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithCancel(ctx)

		root := &proxy.Node{
			Name:       adapter.Name,
			Next:       make(map[string]*proxy.Node),
			IsEndpoint: false,
			Context:    ctx,
			Server:     adapterServer,
		}

		root.BuildNext(http.Name).IsEndpoint = true
		root.BuildNext(socks.Name).IsEndpoint = true

		clientStack := GenerateClientTree(StackOptions{
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
		// TODO(P1): reverse forwards (daemon mode). When reverse.enabled and
		// reverse.forwards are set, build the trojan client directly (without
		// the router/mux layers on top) and call reverse.DialReverse for each
		// forward entry with WithTarget(fwd.Target). Embedded callers already
		// do this themselves; the stock client stack does not expose the inner
		// trojan client instance yet.
		s := proxy.FindAllEndpoints(root)
		return proxy.NewProxy(ctx, cancel, s, c), nil
	})
}
