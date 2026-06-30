---
title: "更新日志"
---

## v1.0.2

发布日期：2026-06-30。CI/CD 基础设施维护与依赖升级，无功能性变更，对终端用户无感知。

### CI/CD 修复

- 修复 `dependabot-auto-merge` 工作流在 **major 版本更新**时误报失败的问题：根因是 `pull_request_target` 事件无 checkout 步骤，`gh label create` 缺少 `--repo` 上下文导致找不到 git 仓库；修复方式为注入 `GH_REPO` 环境变量，并对 major 通知步骤增加 `continue-on-error` 容错
- 合并 Dependabot 累积的依赖更新 PR（#1 checkout、#2 action-gh-release、#3 setup-go、#9 golang.org/x/net）

### 依赖升级

| 依赖 | 旧版本 | 新版本 | 类型 |
|------|--------|--------|------|
| actions/checkout | v4 | v6 | major |
| actions/setup-go | v5 | v6 | major |
| softprops/action-gh-release | v2 | v3 | major |
| golang.org/x/net | 0.55.0 | 0.56.0 | minor |

## v1.0.1

发布日期：2026-06-08。TLS 与 smux 稳定性修复。

- **TLS**：证书加载错误信息补全（上游 Issue #513）
- **smux**：`stickyConn` 改为非阻塞发送，防止 channel 满时 goroutine 死锁
- **兼容性**：处理 `x509.DecryptPEMBlock` 在新版 Go 中的废弃警告
- **CI**：修复 golangci-lint 配置与 shadowsocks 测试超时问题

## v1.0.0

初始正式发布版本。基于 `0.10.6-bugfix` 稳定基线，确立语义化版本号（SemVer）体系与完整的 CI/CD 流水线（Test、Linter、Docker 多架构构建、nightly 构建、自动发布）。详细变更请参见下方 `0.10.6-bugfix` 的完整明细。

## 0.10.6-bugfix

本次更新基于 p4gefau1t/trojan-go-next v0.10.6，精选了安全性和稳定性修复。仅修 bug，不加功能，不改架构。

项目现由 [corevx](https://github.com/corevx) 维护，仓库迁移至 [corevx/trojan-go-next](https://github.com/corevx/trojan-go-next)。

### 重要变更

- Go 版本要求从 **1.14** 提升至 **1.22**
- 所有 `io/ioutil` 调用已迁移至 `io` 和 `os` 包（Go 1.16+ 推荐做法）
- 新增 Docker 多阶段构建支持

### 安全性修复

| 修复项 | 影响范围 | 详情 |
|--------|----------|------|
| 加密私钥加载 | 服务端 | 修复 `x509.DecryptPEMBlock` 在 Go 1.22+ 不可用的问题，恢复加密私钥支持 |
| 私钥解密逻辑反转 | 服务端 | 原代码在解密成功时返回错误，导致加密私钥始终无法使用。现在解密失败时才报错 |
| MySQL 用户删除 | MySQL 模式 | `RowsAffected` 逻辑反转：原代码在 `err != nil` 时检查结果，现已修正为 `err == nil && r == 0`。修复删除用户后仍可连接的问题（Issue #374） |

### 稳定性修复

| 修复项 | 影响范围 | 详情 |
|--------|----------|------|
| Panic 恢复 | 服务端 | 在 trojan/tls/mux 三个 server 的连接处理 goroutine 中添加 `defer recover()`，防止单连接 panic 导致整个服务崩溃 |
| 非阻塞 Channel 发送 | 服务端 | trojan/server.go 中 4 处 channel 发送改为 `select` 非阻塞模式，防止 channel 满时 goroutine 永久阻塞 |
| Ticker 泄漏 | 服务端 | tls/server.go `checkKeyPairLoop` 和 memory.go `speedUpdater` 中添加 `defer ticker.Stop()`，防止 goroutine 泄漏 |

### 功能性修复

| 修复项 | 影响范围 | 详情 |
|--------|----------|------|
| 限速 bug | API + 服务端 | memory.go 中 `else if` 改为 `if`，使 recv 限速器独立于 send 限速器运行。修复通过 API 设定的限速不生效的问题（Issue #216） |
| SNI 错误消息 | 服务端 | SNI 校验失败时的错误消息现在会显示证书的 DNSNames 列表，而不仅显示硬编码的 sni 字段，方便排查（Issue #531, PR #532） |

### 性能优化

| 优化项 | 影响范围 | 详情 |
|--------|----------|------|
| sync.Pool | 全局 | proxy/proxy.go 中 UDP 包缓冲区使用 `sync.Pool` 复用，减少高频场景下的内存分配和 GC 压力 |

### 明确不包含的改动

以下改动来自社区 fork，经评估存在破坏性，**未合并**：

- ~~30s 硬编码超时~~ — 导致客户端 38 秒固定断流（Issue #73）
- ~~net.Dial 替换 underlay.DialConn~~ — 破坏隧道栈分层，WebSocket+TLS 失效
- ~~QUIC 隧道~~ — 新功能，非 bug fix
- ~~SQLite 持久化~~ — 新功能，依赖 CGO
- ~~Recorder 模块~~ — 新功能
- ~~X-Forwarded-For 注入~~ — 新功能

### 验证状态

- 编译：`make clean && make` 通过
- 单元测试：24 个测试包全部通过（`go test -v ./...`）
- 集成测试：server + client SOCKS5 代理 HTTP 流量验证通过
