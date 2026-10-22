package reality

import (
	"github.com/p4gefau1t/trojan-go/config"
)

type Config struct {
	RemoteHost string        `json:"remote_addr" yaml:"remote-addr"`
	RemotePort int           `json:"remote_port" yaml:"remote-port"`
	Reality    RealityConfig `json:"reality" yaml:"reality"`
}

// RealityConfig 客户端 REALITY 参数。字段语义对齐主流生态（Xray/sing-box/mihomo）：
// public-key 与 short-id 由服务端生成后下发（如 `xray x25519`），server-name
// 为伪装的真站域名（ClientHello SNI），fingerprint 复用 tls 层的取值。
type RealityConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	PublicKey   string `json:"public-key" yaml:"public-key"`
	ShortID     string `json:"short-id" yaml:"short-id"`
	ServerName  string `json:"server-name" yaml:"server-name"`
	Fingerprint string `json:"fingerprint" yaml:"fingerprint"`
}

func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return &Config{
			Reality: RealityConfig{
				Fingerprint: "chrome",
			},
		}
	})
}
