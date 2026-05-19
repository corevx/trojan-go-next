# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Trojan-Go-Next is a Go implementation of the Trojan proxy protocol with extensions: WebSocket transport, smux multiplexing, Shadowsocks AEAD encryption, geo-based routing, gRPC API, and TProxy transparent proxy. Compatible with the original Trojan config format.

## Build & Test Commands

```bash
# Build (full feature set, CGO disabled)
make

# Run all tests (SHADOWSOCKS_SF_CAPACITY is required)
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./...

# Run a single package's tests
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./tunnel/trojan/

# Cross-compile releases
make release

# Lint (uses .github/linters/.golangci.yml)
golangci-lint run
```

Build tags control feature inclusion: `full` (everything), `mini` (client+server+forward+nat+mysql), or individual: `client`, `server`, `forward`, `nat`, `custom`, `api`, `mysql`, `other`.

Version/commit are injected via ldflags in the Makefile.

## Architecture

### Layered Tunnel Stack

The core design is a **pluggable tunnel stack** where each layer wraps the next. Tunnels register themselves via `init()` in `tunnel/`. Key interfaces in `tunnel/tunnel.go`:

- `Tunnel` — registers and creates Client/Server
- `Client` — outbound dialer
- `Server` — inbound listener

Data flow on the **client** side (e.g.): `socks/http → adapter → trojan → shadowsocks → tls/websocket → transport → remote`

Data flow on the **server** side: TLS listener branches into direct and WebSocket paths, unwraps trojan/shadowsocks/mux, then routes to `freedom` (direct) or `router`.

### Five Proxy Modes

Each mode is a different tunnel stack composition registered in `proxy/`:

| Mode | Inbound | Outbound | Notes |
|------|---------|----------|-------|
| CLIENT | socks+http adapter | tunnel stack | Standard client |
| SERVER | tls/ws (branching tree) | freedom/router | Standard server |
| FORWARD | dokodemo (any-address) | tunnel stack | Port forwarding |
| NAT | tproxy (Linux-only) | tunnel stack | Transparent proxy |
| CUSTOM | user-defined stack | user-defined stack | Full control |

### Config System

Context-based dependency injection (`config/config.go`). Each package registers a config creator via `config.RegisterConfigCreator()`. Configs are stored as `context.WithValue` and retrieved with `config.FromContext(ctx, name)`. Supports JSON and YAML.

### Component Aggregation

`component/` uses Go build tags to conditionally import proxy modes and services. This is how `make` with `-tags "full"` pulls in everything vs. smaller builds.

### Option Handlers

`option/option.go` implements a priority-based handler chain. `PopOptionHandler()` returns the highest-priority handler: easy mode (50) > version/URL (10) > stdin (0) > config file (-1).

## Git & Release 工作流

- 提交作者：`corevx <corevx@users.noreply.github.com>`
- 每完成一个功能性阶段后立即 commit，不要积攒大量改动后一次性提交
- commit message 使用中文，清晰描述变更意图
- 重要功能性更新或修改，按照语义化版本号（SemVer）更新 `version/` 中的版本号并发布
- 版本号格式：`MAJOR.MINOR.PATCH`（如 `0.11.0`）
  - MAJOR：不兼容的 API 变更
  - MINOR：向后兼容的功能新增
  - PATCH：向后兼容的 bug 修复
- 不主动 push 到远程，除非用户明确要求

## Key Conventions

- GeoIP/GeoSite data files (`.dat`) are loaded from the binary's directory or `TROJAN_GO_LOCATION_ASSET` env var
- Integration tests live in `test/scenario/`
- API is gRPC-based with protobuf definitions in `api/service/`
- The `redirector/` package handles server-side connection fallback
- Server-side Node tree (`proxy/proxy.go`) enables branching to handle both direct TLS and WebSocket simultaneously
