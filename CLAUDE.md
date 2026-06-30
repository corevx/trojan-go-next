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

## CI/CD 依赖自动更新

基于 GitHub Actions 实现依赖定时更新、自动测试、自动合并与发布，由三个文件协同工作：

| 文件 | 职责 |
|------|------|
| `.github/dependabot.yml` | Dependabot 配置：每周一检测 Go modules + GitHub Actions 更新，逐个创建 PR |
| `.github/workflows/dependabot-auto-merge.yml` | Dependabot PR 自动 approve + merge（patch/minor 自动，major 需人工审核） |
| `.github/workflows/deps-update.yml` | 全量更新流水线：定时 `go get -u` → 三平台测试 → 自动合并 → bump PATCH 版本 → 构建发布 |

### deps-update.yml 流程

```
每周一 05:00 触发 → go get -u → 创建 PR → 三平台测试 → 自动合并 → bump 版本 → make release → GitHub Release
```

也可在 Actions 页面手动触发（Run workflow）。

### 前置条件

- GitHub 仓库 Settings → Actions → General 需开启 **Allow GitHub Actions to create and approve pull requests**
- 远程仓库名为 `corevx`（非 `origin`），推送命令为 `git push corevx main`

## 本地写权限与构建注意

- **gh CLI 身份限制**：默认认证为 `toimc`，对 corevx/trojan-go-next 仅 READ 权限。需要写权限的操作（创建 label、上传 release assets 等），从本地 `.env` 加载 corevx 的 `GH_TOKEN`（fine-grained PAT，已 gitignore，**严禁提交**）：
  ```bash
  set -a; source .env; set +a
  gh <写操作> --repo corevx/trojan-go-next
  ```
- **合并 dependabot PR（绕过 gh 权限）**：`git fetch corevx` → 本地 `git merge --no-ff corevx/<分支>` → `git push corevx main`，GitHub 自动把 PR 标记为 MERGED。
- **发版**：`git tag -a vX.Y.Z -m "..."` → `git push corevx vX.Y.Z`，触发 `release-build.yml`（tag `v*.*.*`）+ `docker-build.yml`。
- **本地 `make release`（macOS）**：系统无 `wget`，需改用 `curl` 下载 geo 数据；`github.com/.../raw` 易触发 429 限流，改用 `raw.githubusercontent.com`：
  ```bash
  curl -L -o geoip.dat https://raw.githubusercontent.com/v2fly/geoip/release/geoip.dat
  curl -L -o geosite.dat https://raw.githubusercontent.com/v2fly/domain-list-community/release/dlc.dat
  ```
- **release 产物命名**：Makefile `NAME := trojan-go`，`action-gh-release` 的 `files` glob 必须是 `trojan-go-*.zip`（曾误写为 `trojan-go-next-*.zip`，导致 v1.0.1/v1.0.2 Release 缺二进制，v1.0.2 已修复）。

## Key Conventions

- GeoIP/GeoSite data files (`.dat`) are loaded from the binary's directory or `TROJAN_GO_LOCATION_ASSET` env var
- Integration tests live in `test/scenario/`
- API is gRPC-based with protobuf definitions in `api/service/`
- The `redirector/` package handles server-side connection fallback
- Server-side Node tree (`proxy/proxy.go`) enables branching to handle both direct TLS and WebSocket simultaneously

## Version History

| Version | Date | Changes |
|---------|------|---------|
| v1.0.2 | 2026-06-30 | ci: 修复 dependabot auto-merge major 误报；升级 GitHub Actions（checkout/setup-go v6、action-gh-release v3）；合并依赖更新 PR #1/#2/#3/#9 |
| v1.0.1 | 2026-06-08 | fix: TLS 证书加载错误信息补全（上游 #513）；smux stickyConn 非阻塞防死锁；x509.DecryptPEMBlock 废弃警告 |
| v1.0.0 | - | 初始发布版本 |
