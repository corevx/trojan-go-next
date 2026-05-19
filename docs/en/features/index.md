---
title: Features
---

# Features

Trojan-Go extends the original Trojan protocol with several powerful features.

> **Note:** Using any of these extensions will break compatibility with the original Trojan. Both server and client must run Trojan-Go.

## Overview

| Feature | Description | Docs |
|---------|-------------|------|
| [Multiplexing](/features/mux) | Connection multiplexing via smux for lower latency | 中文 |
| [WebSocket + CDN](/features/websocket) | CDN traffic relay via WebSocket over TLS (Cloudflare guide) | 中文 |
| [Routing](/features/router) | GeoIP/GeoSite-based traffic splitting and ad blocking | 中文 |
| [AEAD Encryption](/features/aead) | Shadowsocks AEAD secondary encryption layer | 中文 |
| [Transport Plugin](/features/plugin) | Pluggable transport layer (SIP003 compatible) | 中文 |
| [Forward Proxy](/features/forward) | Tunnel and reverse proxy, DNS tunneling | 中文 |
| [Transparent Proxy](/features/nat) | TProxy-based transparent proxy (Linux), iptables/nftables | 中文 |
| [SNI Relay](/features/nginx-relay) | Nginx SNI-based multi-path relay | 中文 |
| [Custom Protocol Stack](/features/custom-stack) | User-defined tunnel stack composition | 中文 |
| [URL Share Links](/features/url-scheme) | `trojan-go://` URL scheme for quick config import | 中文 |
| [REST API](/management/rest-api) | HTTP REST API for user management (v0.11.0) | 中文 |
| [Monitoring](/management/monitor) | Health checks and Prometheus metrics (v0.11.0) | 中文 |
| [Prometheus Metrics](/management/metrics) | Metrics output with Grafana integration | 中文 |
| [gRPC API](/management/grpc-api) | CLI-based gRPC API for remote management | 中文 |

> Feature documentation is currently available in Chinese. Click any feature name to view the Chinese documentation.
