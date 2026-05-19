---
layout: home

hero:
  name: Trojan-Go
  text: Secure, Efficient, Easy-to-Use Trojan Proxy
  tagline: A complete Trojan proxy implementation in Go, compatible with the original Trojan protocol and configuration format
  actions:
    - theme: brand
      text: Getting Started
      link: /en/guide/install
    - theme: alt
      text: Configuration
      link: /en/guide/config
    - theme: alt
      text: GitHub
      link: https://github.com/corevx/trojan-go-next

features:
  - icon: 🔒
    title: TLS Tunnel Transport
    details: Traffic identical to HTTPS. Built on mature TLS encryption to resist passive/active detection.
  - icon: ⚡
    title: Multiplexing
    details: Connection multiplexing via smux reduces TLS handshake overhead and lowers latency.
  - icon: 🌐
    title: WebSocket + CDN Relay
    details: CDN traffic relay via WebSocket over TLS, with backward compatibility for non-WebSocket clients.
  - icon: 🛡️
    title: AEAD Encryption
    details: Shadowsocks AEAD secondary encryption layer to prevent CDN traffic inspection.
  - icon: 🔀
    title: Routing & Splitting
    details: Built-in GeoIP/GeoSite routing module with proxy, bypass, and block policies.
  - icon: 📡
    title: REST API & Monitoring
    details: HTTP REST API for user management, health checks, and Prometheus metrics output (v0.11.0).
  - icon: 🔐
    title: TLS Hardening
    details: Certificate expiry detection, minimum TLS version control, and SNI verification (v0.11.0).
  - icon: 📦
    title: Cross-Platform, Zero Dependencies
    details: Single binary for Linux / macOS / Windows / MIPS routers. Docker image available.
---

## Quick Start

### Server

```shell
sudo ./trojan-go -server -remote 127.0.0.1:80 -local 0.0.0.0:443 \
    -key ./your_key.key -cert ./your_cert.crt -password your_password
```

### Client

```shell
./trojan-go -client -remote example.com:443 -local 127.0.0.1:1080 \
    -password your_password
```

### Docker

```shell
docker run --name trojan-go -d \
    -v /etc/trojan-go/:/etc/trojan-go \
    --network host \
    ghcr.io/corevx/trojan-go-next
```

## Compatible Clients

Trojan-Go server is compatible with all clients that support the standard Trojan protocol:

- [v2rayN](https://github.com/2dust/v2rayN) — Windows / macOS / Linux
- [Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev) — Cross-platform
- [NekoBox for Android](https://github.com/MatsuriDayo/NekoBoxForAndroid) — Android
- [ShadowRocket](https://apps.apple.com/app/shadowrocket/id932747118) — iOS
- [sing-box](https://github.com/SagerNet/sing-box) — Universal proxy platform

> **Note:** The clients above support the standard Trojan protocol. Trojan-Go extensions (WebSocket, multiplexing, AEAD) require running the `trojan-go` binary directly.

## Community

For questions, bug reports, or suggestions, join the [Telegram group](https://t.me/trojan_go_chat).

> Across the Great Wall, we can reach every corner in the world.
