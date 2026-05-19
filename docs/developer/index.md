---
title: 开发者入门
---

# 开发者入门

欢迎参与 Trojan-Go-Next 开发。本文帮助你快速了解项目结构和贡献流程。

## 技术栈

| 技术 | 用途 |
|------|------|
| Go 1.25+ | 主语言 |
| gRPC + Protobuf | API 定义 |
| utls | TLS 指纹伪造 |
| smux | 多路复用 |
| V2Fly GeoIP/GeoSite | 路由数据 |
| VitePress | 文档站 |

## 项目结构速览

```
trojan-go-next/
├── proxy/        # 5 种代理模式（client/server/forward/nat/custom）
├── tunnel/       # 15 个隧道层（tls/websocket/trojan/mux/router/...）
├── api/          # gRPC + REST API + 监控服务
├── statistic/    # 用户认证（内存/MySQL）
├── config/       # 配置系统（context 依赖注入）
├── component/    # 构建标签（控制功能模块）
├── option/       # 命令行参数处理
├── redirector/   # 服务端连接回退
├── log/          # 日志系统
├── metric/       # 指标系统
├── easy/         # 简易模式
├── url/          # URL 分享链接
├── version/      # 版本信息
└── docs/         # VitePress 文档
```

## 5 分钟理解核心设计

Trojan-Go-Next 采用**可插拔隧道栈**架构：

```
入站层 → 协议层 → 加密层 → 传输层 → 出站
```

每一层实现 `tunnel.Tunnel` 接口，通过 `init()` 自动注册。5 种代理模式组合不同的隧道栈。

详见 [架构概览](/developer/overview)。

## 开发流程

### 1. 环境准备

```shell
git clone https://github.com/corevx/trojan-go-next-next.git
cd trojan-go-next
make
```

### 2. 运行测试

```shell
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./...
```

### 3. 代码规范

项目使用 golangci-lint v2 进行代码检查：

```shell
golangci-lint run --config=.github/linters/.golangci.yml
```

### 4. 构建标签

功能通过构建标签控制：

| 标签 | 包含功能 |
|------|---------|
| `full` | 全部功能 |
| `mini` | client+server+forward+nat+mysql |
| `client` | 仅客户端 |
| `server` | 仅服务端 |
| `api` | gRPC+REST+监控 |

## 接下来

- [架构概览](/developer/overview) — 理解隧道栈设计
- [架构设计](/developer/architecture) — 高层系统设计
- [编译与构建](/developer/build) — 完整的构建和测试指南
- [隧道 API 参考](/developer/tunnel-api) — tunnel.Tunnel 接口详解
- [传输层插件开发](/developer/plugin-dev) — 开发自定义传输层
