# Trojan-Go

A complete Trojan proxy implementation in Go, compatible with the original Trojan protocol and configuration format. Secure, efficient, lightweight, and easy to use.

Trojan-Go supports [multiplexing](#multiplexing) to improve concurrency performance, a built-in [routing module](#routing) for traffic splitting, [CDN traffic relay](#websocket) via WebSocket over TLS, [secondary encryption](#shadowsocks-aead-encryption) using Shadowsocks AEAD, and pluggable [transport layer plugins](#transport-plugin).

Pre-built binaries are available on the [Release page](https://github.com/corevx/trojan-go-next/releases). Just download, extract, and run — no additional dependencies required.

For questions, bug reports, or suggestions, join the [Telegram group](https://t.me/trojan_go_chat).

**Full documentation: [Trojan-Go Docs](https://corevx.github.io/trojan-go-next)**

## Features

### Compatible with Original Trojan

- TLS tunnel transport
- UDP proxy
- Transparent proxy (NAT mode, see [iptables setup](https://github.com/shadowsocks/shadowsocks-libev/tree/v3.3.1#transparent-proxy))
- Anti-GFW passive/active detection mechanisms
- MySQL data persistence
- MySQL user authentication
- User traffic statistics and quota limits

### Extended Features

- Easy mode for quick deployment
- Socks5 / HTTP proxy auto-detection
- TProxy-based transparent proxy (TCP / UDP)
- Cross-platform, no special dependencies
- Multiplexing (smux) for lower latency and higher concurrency
- Custom routing module for traffic splitting and ad blocking
- WebSocket transport for CDN relay and anti-GFW MITM attacks
- TLS fingerprint spoofing against TLS Client Hello inspection
- gRPC API for user management and speed limiting
- Pluggable transport layer (replace TLS with other protocols or plaintext)
- Shadowsocks SIP003 plugin support
- YAML configuration format support

### GUI Clients

Trojan-Go server is compatible with all clients that support the standard Trojan protocol. The following actively maintained clients work with Trojan-Go:

- [v2rayN](https://github.com/2dust/v2rayN) — Windows / macOS / Linux client (Xray / sing-box core)
- [Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev) — Cross-platform client (Windows / macOS / Linux) based on Mihomo
- [NekoBox for Android](https://github.com/MatsuriDayo/NekoBoxForAndroid) — Android client based on sing-box
- [ShadowRocket](https://apps.apple.com/app/shadowrocket/id932747118) — iOS client
- [sing-box](https://github.com/SagerNet/sing-box) — Universal proxy platform (CLI / library)

> **Note:** The above clients support the standard Trojan protocol. Trojan-Go extensions (WebSocket transport, smux multiplexing, Shadowsocks AEAD secondary encryption) require running the `trojan-go` binary directly. The previous GUI clients with built-in Trojan-Go core ([Qv2ray](https://github.com/Qv2ray/Qv2ray) and [Igniter-Go](https://github.com/p4gefau1t/trojan-go-android)) are no longer maintained.

## Quick Start

### Easy Mode

Server:

```shell
sudo ./trojan-go -server -remote 127.0.0.1:80 -local 0.0.0.0:443 \
    -key ./your_key.key -cert ./your_cert.crt -password your_password
```

Client:

```shell
./trojan-go -client -remote example.com:443 -local 127.0.0.1:1080 \
    -password your_password
```

### Config File Mode

```shell
./trojan-go -config config.json
```

### URL Mode

```shell
./trojan-go -url 'trojan-go://password@cloudflare.com/?type=ws&path=%2Fpath&host=your-site.com'
```

### Docker

```shell
docker run --name trojan-go -d \
    -v /etc/trojan-go/:/etc/trojan-go \
    --network host \
    ghcr.io/corevx/trojan-go-next
```

Or with a custom config path:

```shell
docker run --name trojan-go -d \
    -v /path/to/host/config:/path/in/container \
    --network host \
    ghcr.io/corevx/trojan-go-next \
    /path/in/container/config.json
```

## Configuration

### Minimal Server (`server.json`)

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

### Minimal Client (`client.json`)

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

### YAML Format (`client.yaml`)

```yaml
run-type: client
local-addr: 127.0.0.1
local-port: 1080
remote-addr: www.example.com
remote-port: 443
password:
  - your_password
```

## Feature Details

> Using any of the extended features below (multiplexing, WebSocket, etc.) will break compatibility with the original Trojan.

### WebSocket

Enable WebSocket in both client and server config to relay traffic through a CDN:

```json
"websocket": {
    "enabled": true,
    "path": "/your-websocket-path",
    "hostname": "www.example.com"
}
```

The server supports WebSocket and plain Trojan traffic simultaneously. Clients without WebSocket config still work. Both sides must use Trojan-Go to actually use WebSocket transport.

### Multiplexing

Reduces TCP/TLS handshake overhead by multiplexing connections over a single TLS tunnel (based on [smux](https://github.com/xtaci/smux)). Enable on the client side only — the server auto-detects.

> Mux does not increase raw throughput, but lowers latency and improves experience under high concurrency (e.g., image-heavy web pages).

```json
"mux": {
    "enabled": true
}
```

### Routing

Built-in routing module with three policies:

- **Proxy** — route through the TLS tunnel
- **Bypass** — connect directly
- **Block** — drop the connection

```json
"router": {
    "enabled": true,
    "bypass": ["geoip:cn", "geoip:private", "full:localhost"],
    "block": ["cidr:192.168.1.1/24"],
    "proxy": ["domain:google.com"],
    "default_policy": "proxy"
}
```

### Shadowsocks AEAD Encryption

Secondary encryption of Trojan traffic to prevent CDN inspection:

```json
"shadowsocks": {
    "enabled": true,
    "password": "my-password"
}
```

Must be enabled on both server and client with the same password.

### Transport Plugin

Pluggable transport layer with Shadowsocks SIP003 plugin support. Example with `v2ray-plugin`:

> **This config is for demonstration only — not secure.**

Server:

```json
"transport_plugin": {
    "enabled": true,
    "type": "shadowsocks",
    "command": "./v2ray-plugin",
    "arg": ["-server", "-host", "www.example.com"]
}
```

Client:

```json
"transport_plugin": {
    "enabled": true,
    "type": "shadowsocks",
    "command": "./v2ray-plugin",
    "arg": ["-host", "www.example.com"]
}
```

## Build

> Requires Go >= 1.14

```shell
git clone https://github.com/corevx/trojan-go-next.git
cd trojan-go
make
make install  # optional: install systemd service
```

Or build directly:

```shell
go build -tags "full"
```

Cross-compile examples:

```shell
# Windows 64-bit
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags "full"

# macOS Apple Silicon
CGO_ENABLED=0 GOOS=macos GOARCH=arm64 go build -tags "full"

# Linux 64-bit
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags "full"

# Minimal client for MIPS (router/IoT)
CGO_ENABLED=0 GOOS=linux GOARCH=mips go build -tags "client" -trimpath -ldflags "-s -w -buildid="
```

Build tags: `full` (everything), `mini` (client+server+forward+nat+mysql), or individual: `client`, `server`, `forward`, `nat`, `custom`, `api`, `mysql`, `other`.

## Architecture

Trojan-Go uses a **pluggable tunnel stack** where each layer wraps the next. Tunnels register via `init()` in `tunnel/`. Five proxy modes compose different tunnel stacks:

| Mode    | Inbound              | Outbound       | Notes              |
|---------|----------------------|----------------|---------------------|
| CLIENT  | socks+http adapter   | tunnel stack   | Standard client     |
| SERVER  | tls/ws (branching)   | freedom/router | Standard server     |
| FORWARD | dokodemo (any-addr)  | tunnel stack   | Port forwarding     |
| NAT     | tproxy (Linux-only)  | tunnel stack   | Transparent proxy   |
| CUSTOM  | user-defined         | user-defined   | Full control        |

## Acknowledgements

- [Trojan](https://github.com/trojan-gfw/trojan)
- [V2Fly](https://github.com/v2fly)
- [utls](https://github.com/refraction-networking/utls)
- [smux](https://github.com/xtaci/smux)
- [go-tproxy](https://github.com/LiamHaworth/go-tproxy)

## License

[GPL-3.0](LICENSE)

[简体中文](README_cn.md)
