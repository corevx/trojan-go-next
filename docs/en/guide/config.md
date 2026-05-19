---
title: Configuration
---

# Configuration Guide

Trojan-Go supports both JSON and YAML configuration formats. They are functionally identical.

## Configuration Structure

A complete configuration file consists of these sections:

| Section | Purpose |
|---------|---------|
| Basic | `run_type`, `local_addr`, `local_port`, `remote_addr`, `remote_port`, `password` |
| SSL | TLS certificate, SNI, fallback |
| WebSocket | CDN relay settings |
| Mux | Multiplexing settings |
| Shadowsocks | AEAD secondary encryption |
| Router | Traffic routing and splitting |
| Transport Plugin | Pluggable transport layer |
| MySQL | User authentication via MySQL |
| API | gRPC API settings |

## Minimal Server Config

```json
{
    "run_type": "server",
    "local_addr": "0.0.0.0",
    "local_port": 443,
    "remote_addr": "127.0.0.1",
    "remote_port": 80,
    "password": ["your_password"],
    "ssl": {
        "cert": "server.crt",
        "key": "server.key"
    }
}
```

### Key Fields

- **`remote_addr` / `remote_port`**: The local HTTP server address. Trojan-Go forwards non-Trojan traffic here. Must be running, or Trojan-Go will refuse to start.
- **`ssl.fallback_port`**: (Optional) Port for non-TLS connections. Recommended to return a "400 Bad Request" page.

## Minimal Client Config

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "example.com",
    "remote_port": 443,
    "password": ["your_password"]
}
```

### Key Fields

- **`local_addr` / `local_port`**: Local SOCKS5/HTTP proxy listen address.
- **`remote_addr`**: Your server domain or IP. If using a domain, `ssl.sni` can be omitted.
- **`ssl.sni`**: Server Name Indication. Required when `remote_addr` is an IP address. Transmitted in plaintext during TLS handshake — avoid using blocked domains.

## YAML Format

```yaml
run-type: client
local-addr: 127.0.0.1
local-port: 1080
remote-addr: example.com
remote-port: 443
password:
  - your_password
```

## Common Options

### Multi-User Server

```json
{
    "run_type": "server",
    "password": [
        "password_for_user_1",
        "password_for_user_2"
    ]
}
```

### WebSocket for CDN Relay

```json
{
    "websocket": {
        "enabled": true,
        "path": "/your-websocket-path",
        "hostname": "www.example.com"
    }
}
```

### Multiplexing (Client Only)

```json
{
    "mux": {
        "enabled": true
    }
}
```

### Routing

```json
{
    "router": {
        "enabled": true,
        "bypass": ["geoip:cn", "geoip:private"],
        "block": ["geosite:category-ads-all"],
        "proxy": ["domain:google.com"],
        "default_policy": "proxy"
    }
}
```

### AEAD Encryption

```json
{
    "shadowsocks": {
        "enabled": true,
        "password": "my-encryption-password"
    }
}
```

::: warning
AEAD encryption must be enabled on both server and client with the same password.
:::

## Next Steps

- [Full Configuration Reference (中文)](/guide/full-config) — Complete config file with all options
- [WebSocket CDN Relay (中文)](/features/websocket) — CDN traffic relay setup
- [Routing (中文)](/features/router) — Traffic splitting and ad blocking
