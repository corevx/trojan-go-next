package shadowtls

import (
	"github.com/p4gefau1t/trojan-go/config"
)

type Config struct {
	ShadowTLS ShadowTLSConfig `json:"shadowtls" yaml:"shadowtls"`
}

type ShadowTLSConfig struct {
	Enabled     bool            `json:"enabled" yaml:"enabled"`
	Version     int             `json:"version" yaml:"version"`
	Users       []UserConfig    `json:"users" yaml:"users"`
	Handshake   HandshakeConfig `json:"handshake" yaml:"handshake"`
	Strict      bool            `json:"strict" yaml:"strict"`
	WildcardSNI string          `json:"wildcard_sni" yaml:"wildcard-sni"`
}

// UserConfig v3 用户：密码即身份，验签按用户逐个尝试，命中者名字记入日志
type UserConfig struct {
	Name     string `json:"name" yaml:"name"`
	Password string `json:"password" yaml:"password"`
}

// HandshakeConfig 真站（被伪装的第三方 TLS 站点）地址。服务端把未验签
// 连接与真站握手中继的全部流量转发到此地址。
type HandshakeConfig struct {
	Server     string `json:"server" yaml:"server"`
	ServerPort int    `json:"server_port" yaml:"server-port"`
}

// 组合约束（在 proxy/server 组装点强制）：
//   - shadowtls 与 websocket 互斥：真站握手中继与 WS 分流树语义冲突
//   - shadowtls 与 transport_plugin 互斥：插件已剥离传输层加密职责
//   - shadowtls 与 ssl 层互斥：栈组合固定为 transport→shadowtls→trojan，
//     外层 TLS 由真站握手中继提供，本进程不再终结 TLS
//
// 服务端配置样例（与 nginx stream SNI 分流共存，nginx 侧不做 TLS 终结）：
//
//	run-type: server
//	local-addr: 0.0.0.0
//	local-port: 8444          # 对外经 nginx 443 ssl_preread 转发到此
//	password:
//	    - trojan-pass          # 内层 trojan 用户密码
//	shadowtls:
//	    enabled: true
//	    version: 3
//	    users:
//	        - name: u1
//	          password: shadowtls-pass
//	    handshake:
//	        server: www.example.com   # 真站
//	        server-port: 443
//	    strict: true
//
// nginx 共存（stream 层按 SNI 分流，ssl_preread 不终结 TLS）：
//
//	stream {
//	    map $ssl_preread_server_name $backend {
//	        www.example.com   127.0.0.1:8444;   # 本服务 shadowtls 端口
//	        default           127.0.0.1:9443;   # 其他 SNI → 真实业务
//	    }
//	    server {
//	        listen 443 reuseport;
//	        proxy_pass $backend;
//	        ssl_preread on;
//	    }
//	}
func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return &Config{
			ShadowTLS: ShadowTLSConfig{
				Version:     3,
				Strict:      true,
				WildcardSNI: "off",
			},
		}
	})
}
