---
title: Features
---

# Features

Trojan-Go extends the original Trojan protocol with several powerful features.

> **Note:** Using any of these extensions will break compatibility with the original Trojan. Both server and client must run Trojan-Go.

## Overview

| Feature | Description |
|---------|-------------|
| Multiplexing | Connection multiplexing via smux for lower latency |
| WebSocket + CDN | CDN traffic relay via WebSocket over TLS |
| Routing | GeoIP/GeoSite-based traffic splitting and ad blocking |
| AEAD Encryption | Shadowsocks AEAD secondary encryption |
| Transport Plugin | Pluggable transport layer (SIP003 compatible) |
| Forward & Reverse Proxy | Tunnel and reverse proxy support |
| Transparent Proxy | TProxy-based transparent proxy (Linux) |
| SNI Relay | Nginx SNI-based relay |
| Custom Protocol Stack | User-defined tunnel stack composition |
| REST API | HTTP REST API for management (v0.11.0) |
| Monitoring | Health checks and Prometheus metrics (v0.11.0) |

> Feature documentation is currently available in [Chinese (中文)](/features/mux). English translations are coming soon.
