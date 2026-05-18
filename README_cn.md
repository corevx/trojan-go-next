# Trojan-Go

使用 Go 实现的完整 Trojan 代理，兼容原版 Trojan 协议及配置文件格式。安全、高效、轻巧、易用。

Trojan-Go 支持[多路复用](#多路复用)提升并发性能；使用[路由模块](#路由模块)实现国内外分流；支持 [CDN 流量中转](#websocket)（基于 WebSocket over TLS）；支持使用 AEAD 对 Trojan 流量进行[二次加密](#aead-加密)（基于 Shadowsocks AEAD）；支持可插拔的[传输层插件](#传输层插件)，允许替换 TLS，使用其他加密隧道传输 Trojan 协议流量。

预编译二进制可执行文件可在 [Release 页面](https://github.com/corevx/trojan-go-next/releases)下载。解压后即可直接运行，无其他组件依赖。

如遇到配置和使用问题、发现 bug，或是有更好的想法，欢迎加入 [Telegram 交流反馈群](https://t.me/trojan_go_chat)。

**完整介绍和配置教程，参见 [Trojan-Go 文档](https://corevx.github.io/trojan-go-next)。**

## 特性

### 兼容原版 Trojan

- TLS 隧道传输
- UDP 代理
- 透明代理（NAT 模式，iptables 设置参考[这里](https://github.com/shadowsocks/shadowsocks-libev/tree/v3.3.1#transparent-proxy)）
- 对抗 GFW 被动检测 / 主动检测的机制
- MySQL 数据持久化方案
- MySQL 用户权限认证
- 用户流量统计和配额限制

### 扩展功能

- 便于快速部署的「简易模式」
- Socks5 / HTTP 代理自动适配
- 基于 TProxy 的透明代理（TCP / UDP）
- 全平台支持，无特殊依赖
- 基于多路复用（smux）降低延迟，提升并发性能
- 自定义路由模块，可实现国内外分流 / 广告屏蔽等功能
- WebSocket 传输支持，以实现 CDN 流量中转和对抗 GFW 中间人攻击
- TLS 指纹伪造，以对抗 GFW 针对 TLS Client Hello 的特征识别
- 基于 gRPC 的 API 支持，以实现用户管理和速度限制等
- 可插拔传输层，可将 TLS 替换为其他协议或明文传输，同时有完整的 Shadowsocks 混淆插件支持
- 支持更简明易读的 YAML 配置文件格式

### 图形界面客户端

Trojan-Go 服务端兼容所有原 Trojan 客户端，如 Igniter、ShadowRocket 等。以下是支持 Trojan-Go 扩展特性（WebSocket / Mux 等）的客户端：

- [Qv2ray](https://github.com/Qv2ray/Qv2ray)：跨平台客户端，支持 Windows / macOS / Linux，使用 Trojan-Go 核心，支持所有 Trojan-Go 扩展特性。
- [Igniter-Go](https://github.com/p4gefau1t/trojan-go-android)：Android 客户端，Fork 自 Igniter，将核心替换为 Trojan-Go，支持所有 Trojan-Go 扩展特性。

## 快速开始

### 简易模式

服务端：

```shell
sudo ./trojan-go -server -remote 127.0.0.1:80 -local 0.0.0.0:443 \
    -key ./your_key.key -cert ./your_cert.crt -password your_password
```

客户端：

```shell
./trojan-go -client -remote example.com:443 -local 127.0.0.1:1080 \
    -password your_password
```

### 配置文件模式

```shell
./trojan-go -config config.json
```

### URL 模式

```shell
./trojan-go -url 'trojan-go://password@cloudflare.com/?type=ws&path=%2Fpath&host=your-site.com'
```

### Docker 部署

```shell
docker run --name trojan-go -d \
    -v /etc/trojan-go/:/etc/trojan-go \
    --network host \
    ghcr.io/corevx/trojan-go-next
```

或指定配置文件路径：

```shell
docker run --name trojan-go -d \
    -v /path/to/host/config:/path/in/container \
    --network host \
    ghcr.io/corevx/trojan-go-next \
    /path/in/container/config.json
```

## 配置文件

### 最简服务端配置（`server.json`）

```json
{
  "run_type": "server",
  "local_addr": "0.0.0.0",
  "local_port": 443,
  "remote_addr": "127.0.0.1",
  "remote_port": 80,
  "password": ["your_password"],
  "ssl": {
    "cert": "your_cert.crt",
    "key": "your_key.key",
    "sni": "www.example.com"
  }
}
```

### 最简客户端配置（`client.json`）

```json
{
  "run_type": "client",
  "local_addr": "127.0.0.1",
  "local_port": 1080,
  "remote_addr": "www.example.com",
  "remote_port": 443,
  "password": ["your_password"]
}
```

### YAML 格式（`client.yaml`）

```yaml
run-type: client
local-addr: 127.0.0.1
local-port: 1080
remote-addr: www.example.com
remote-port: 443
password:
  - your_password
```

## 功能详解

> 使用以下扩展特性（多路复用、WebSocket 等）后将无法与原版 Trojan 兼容。

### WebSocket

Trojan-Go 支持使用 TLS + WebSocket 承载 Trojan 协议，使得利用 CDN 进行流量中转成为可能。在服务端和客户端配置文件中同时添加 `websocket` 选项即可启用：

```json
"websocket": {
    "enabled": true,
    "path": "/your-websocket-path",
    "hostname": "www.example.com"
}
```

服务端开启 WebSocket 支持后，可以同时支持 WebSocket 和一般 Trojan 流量。未配置 WebSocket 选项的客户端依然可以正常使用。但要使用 WebSocket 承载流量，请确保双方都使用 Trojan-Go。

### 多路复用

在较差的网络条件下，TLS 握手可能耗时较长。Trojan-Go 支持多路复用（基于 [smux](https://github.com/xtaci/smux)），通过一条 TLS 隧道承载多条 TCP 连接，减少握手延迟，提升高并发场景下的性能。

> 启用多路复用不能提高单连接吞吐量，但能降低延迟、提升大量并发请求时的网络体验（如浏览含大量图片的网页）。

只需在客户端开启，服务端会自动检测并提供支持：

```json
"mux": {
    "enabled": true
}
```

### 路由模块

Trojan-Go 客户端内建路由模块，方便实现国内直连、海外代理等自定义路由功能。路由策略有三种：

- **Proxy（代理）**：通过 TLS 隧道代理，由服务端与目标地址建立连接
- **Bypass（绕过）**：本地直接连接目标地址
- **Block（封锁）**：直接关闭连接

```json
"router": {
    "enabled": true,
    "bypass": ["geoip:cn", "geoip:private", "full:localhost"],
    "block": ["cidr:192.168.1.1/24"],
    "proxy": ["domain:google.com"],
    "default_policy": "proxy"
}
```

### AEAD 加密

Trojan-Go 支持基于 Shadowsocks AEAD 对 Trojan 流量进行二次加密，防止不可信的 CDN 识别和审查流量：

```json
"shadowsocks": {
    "enabled": true,
    "password": "my-password"
}
```

服务端和客户端必须同时开启并使用相同的密码。

### 传输层插件

Trojan-Go 支持可插拔的传输层插件，并兼容 Shadowsocks [SIP003](https://shadowsocks.org/en/wiki/Plugin.html) 标准混淆插件。以 `v2ray-plugin` 为例：

> **此配置仅作为演示，不安全。**

服务端：

```json
"transport_plugin": {
    "enabled": true,
    "type": "shadowsocks",
    "command": "./v2ray-plugin",
    "arg": ["-server", "-host", "www.example.com"]
}
```

客户端：

```json
"transport_plugin": {
    "enabled": true,
    "type": "shadowsocks",
    "command": "./v2ray-plugin",
    "arg": ["-host", "www.example.com"]
}
```

## 构建

> 要求 Go >= 1.14

```shell
git clone https://github.com/corevx/trojan-go-next.git
cd trojan-go
make
make install  # 可选：安装 systemd 服务
```

或直接编译：

```shell
go build -tags "full"
```

交叉编译示例：

```shell
# Windows 64 位
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags "full"

# macOS Apple Silicon
CGO_ENABLED=0 GOOS=macos GOARCH=arm64 go build -tags "full"

# Linux 64 位
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags "full"

# MIPS 路由器精简客户端
CGO_ENABLED=0 GOOS=linux GOARCH=mips go build -tags "client" -trimpath -ldflags "-s -w -buildid="
```

Build tag 说明：`full`（全部功能）、`mini`（client+server+forward+nat+mysql），或单独指定：`client`、`server`、`forward`、`nat`、`custom`、`api`、`mysql`、`other`。

## 架构

Trojan-Go 采用**可插拔隧道栈**设计，每层包装下一层。隧道通过 `tunnel/` 目录中的 `init()` 注册。五种代理模式组合不同的隧道栈：

| 模式    | 入站                 | 出站           | 说明       |
|---------|----------------------|----------------|------------|
| CLIENT  | socks+http adapter   | tunnel stack   | 标准客户端 |
| SERVER  | tls/ws（分支树）     | freedom/router | 标准服务端 |
| FORWARD | dokodemo（任意地址） | tunnel stack   | 端口转发   |
| NAT     | tproxy（仅 Linux）   | tunnel stack   | 透明代理   |
| CUSTOM  | 自定义               | 自定义         | 完全控制   |

## 致谢

- [Trojan](https://github.com/trojan-gfw/trojan)
- [V2Fly](https://github.com/v2fly)
- [utls](https://github.com/refraction-networking/utls)
- [smux](https://github.com/xtaci/smux)
- [go-tproxy](https://github.com/LiamHaworth/go-tproxy)

## 许可证

[GPL-3.0](LICENSE)

[English](README.md)
