---
title: "更新日志"
---

## v1.1.2

发布日期：2026-10-22。P2 可靠性收尾修复：错误链穿透、TLS 会话恢复、客户端 ALPN。

- **错误链**：`common.Error` 此前仅拼接错误字符串（被包装的 error 对象被丢弃），现保存 cause 并实现 `Unwrap()`——`errors.Is/As` 可穿透整个包装链，`Error()` 输出格式不变。`proxy` 与 `transport` 中两处字符串后缀匹配的临时方案退化为精确的 `errors.Is` 判断
- **TLS 服务端**：tls.Config 改为全连接共享（原实现每连接新建）。session ticket key 存于 Config 实例内部，逐连接配置使每个 ticket 只在单个连接内有效，**会话恢复（session resumption）从未生效**；修复后 `reuse_session` 配置真正兑现，减少全量握手的 1-RTT 与非对称运算开销。附跨连接恢复的回归测试（变异验证：逐连接配置下必红）
- **TLS 客户端**：utls 与标准库两条路径均传递 ALPN（`NextProtos`），缺省 `http/1.1`（WebSocket 依赖 HTTP/1.1，避免协商出 h2 破坏握手）。utls 指纹路径此前完全忽略 ALPN——真实浏览器 ClientHello 必带该扩展，伪装指纹缺失 ALPN 本身即指纹矛盾点。cipher/session 在指纹路径仍被有意忽略（指纹固定语义，非缺陷）

## v1.1.1

发布日期：2026-10-22。入站链可靠性修复：消除「新连接全挂、既有连接正常、进程存活、只能重启」类故障的全部已知代码级成因。每项修复均附经变异验证的回归测试（对旧代码必红）。

### P0 — 可致服务整体不可用

| 修复项 | 缺陷 | 后果 |
|--------|------|------|
| transport accept 容错 | 对任意 Accept 错误（含瞬时 EMFILE/ENFILE）log 后直接 return，**永久停止 accept** | fd 压力波动一次即触发「端口在听但新连接无人接收」，不可自愈。现仅 `net.ErrClosed`/ctx 退出，其余指数退避（5ms→1s）持续重试 |
| TLS 入站链 deadline | 每连接 goroutine 全程零 SetDeadline | 建连后不发（或半发）ClientHello 的静默连接**永久**占用 goroutine。现握手与请求解析各设 30s 超时，协议识别后清除 |
| 裸 channel 发送 | `connChan`/`wsChan`（容量 32）无 ctx 保护发送 | 下游停滞时每个新连接的 goroutine 持 fd 阻塞堆积直至 EMFILE。现 `select`+`ctx.Done()`，退出路径关连接 |
| WebSocket 畸形请求卡滞 | 带 Upgrade 头但缺 `Sec-WebSocket-Key` 的请求使 x/net/websocket 静默返回，AcceptConn 必等满随机 5-15s 超时 | 上层串行调用，低速率恶意流即可长期占死 ws 接受路径并级联 fd 堆积。现 ServeHTTP 任意退出立即解除等待 |
| simplesocks 串行读头 | acceptLoop 串行对每条 smux 流读协议头，无超时 | 一条开流不发头的连接卡死全部新 mux 流。现 per-stream goroutine 化（不用 SetDeadline——deadline 会透传杀死整个 smux 会话） |

### P1 — 资源与噪音

| 修复项 | 缺陷 |
|--------|------|
| checkKeyPairLoop busy-loop | 证书文件暂时不可读（acme.sh 替换窗口/挂载抖动）时三个 error 分支的 `continue` 跳过 ticker 等待，无间隔自旋烧满一个核+日志刷屏。实测旧代码 225ms 内重试 125,894 次，修复后受 checkRate 节流 |
| UDP 会话关闭误报 | trojan UDP 会话无显式拆链消息，客户端关连接产生的 io.EOF 被当 ERROR 高频刷屏。EOF/ErrClosed/ECONNRESET 降为 debug，`unexpected EOF`（截断帧，真异常）仍报 ERROR |
| ws 超时路径泄漏 | 握手超时后连接不关闭、Handler goroutine 阻塞在无缓冲 channel。现 chan 缓冲 1 + 超时关连接 |

### 构建

- Dockerfile builder 升级 golang:1.26-alpine（go.mod 已要求 1.26.0）
- 新增 `Dockerfile.runtime`：本地交叉编译二进制+geo 数据直接打包的轻量运行时镜像（完整多阶段构建需在容器内拉取私有依赖，无凭证不可行）
- scenario 集成测试的内联证书改为运行时自签（原证书 2026-09-13 过期导致全量测试必红）

## v1.1.0

发布日期：2026-10-11。反向端口转发与反 DPI 指纹能力。

### 新特性

- **reverse 反向端口转发**：新增 `tunnel/reverse` 包与控制协议（trojan 命令字 Reverse/4），支持服务端反向穿透客户端侧端口；`ReverseListener` 暴露 `Connected()` 会话健康面，供上层换路看门狗区分「隧道活着但上级僵死」与正常瞬断；流级闲置掐断（`stream_idle_timeout`）与 session 健康检测解耦；附进程内全链路测试（回显/ACL/冲突/重连/泄漏）
- **smux 帧级随机化（反 DPI）**：帧切分点从 `[frame_min, frame_max]` 均匀随机（默认关闭），打破固定分片→固定 TLS record 周期的可统计指纹；keepalive NOP 间隔 ±jitter 随机（默认关闭）打散固定周期信标。线上协议零改动，旧对端照常解析
- **TLS 客户端证书指纹钉死**（`cert_pin_sha256`）：握手后校验叶子证书 SHA-256，支持冒号分组与裸 hex。级联叶子连自签跳板时比裸关校验安全

### 修复

- reverse bind 握手超时 10s→30s（高负载下测试 1/10 复现误判）
- reverse 配置遵循 trojan-go 顶层节约定
- 测试硬编码证书全量换运行时自签（2026-09-13 过期）

### 依赖

合并 Dependabot 累积 PR：grpc 1.83.2、x/net 0.59.0、protobuf 1.36.12、mysql 1.10.1、testify 1.12.1 等

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
