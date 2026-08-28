package server

import (
	"context"

	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/proxy"
	"github.com/p4gefau1t/trojan-go/proxy/client"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/mux"
	"github.com/p4gefau1t/trojan-go/tunnel/reverse"
	"github.com/p4gefau1t/trojan-go/tunnel/router"
	"github.com/p4gefau1t/trojan-go/tunnel/shadowsocks"
	"github.com/p4gefau1t/trojan-go/tunnel/simplesocks"
	"github.com/p4gefau1t/trojan-go/tunnel/tls"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
	"github.com/p4gefau1t/trojan-go/tunnel/trojan"
	"github.com/p4gefau1t/trojan-go/tunnel/websocket"
)

const Name = "SERVER"

func init() {
	proxy.RegisterProxyCreator(Name, func(ctx context.Context) (*proxy.Proxy, error) {
		cfg := config.FromContext(ctx, Name).(*client.Config)
		ctx, cancel := context.WithCancel(ctx)
		transportServer, err := transport.NewServer(ctx, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		clientStack := []string{freedom.Name}
		if cfg.Router.Enabled {
			clientStack = []string{freedom.Name, router.Name}
		}

		root := &proxy.Node{
			Name:       transport.Name,
			Next:       make(map[string]*proxy.Node),
			IsEndpoint: false,
			Context:    ctx,
			Server:     transportServer,
		}

		if !cfg.TransportPlugin.Enabled {
			root = root.BuildNext(tls.Name)
		}

		trojanSubTree := root
		if cfg.Shadowsocks.Enabled {
			trojanSubTree = trojanSubTree.BuildNext(shadowsocks.Name)
		}
		trojanNode := trojanSubTree.BuildNext(trojan.Name)
		trojanNode.BuildNext(mux.Name).BuildNext(simplesocks.Name).IsEndpoint = true
		trojanNode.IsEndpoint = true

		wsSubTree := root.BuildNext(websocket.Name)
		if cfg.Shadowsocks.Enabled {
			wsSubTree = wsSubTree.BuildNext(shadowsocks.Name)
		}
		wsTrojanNode := wsSubTree.BuildNext(trojan.Name)
		wsTrojanNode.BuildNext(mux.Name).BuildNext(simplesocks.Name).IsEndpoint = true
		wsTrojanNode.IsEndpoint = true

		serverList := proxy.FindAllEndpoints(root)
		clientList, err := proxy.CreateClientStack(ctx, clientStack)
		if err != nil {
			cancel()
			return nil, err
		}
		if reverseCfg, ok := config.FromContext(ctx, reverse.Name).(*reverse.Config); ok && reverseCfg.Reverse.Enabled {
			// the reverse server consumes reverse-command trojan connections
			// and runs autonomously beside the node tree (like trojan's own
			// acceptLoop), so it is not registered as an endpoint
			reverseServer, err := reverse.NewServer(ctx, trojanNode.Server)
			if err != nil {
				cancel()
				return nil, err
			}
			// both trojan servers (direct TLS path and websocket path) share
			// the same port ownership registry
			reverseServer.ListenOn(wsTrojanNode.Server)
		}
		return proxy.NewProxy(ctx, cancel, serverList, clientList), nil
	})
}
