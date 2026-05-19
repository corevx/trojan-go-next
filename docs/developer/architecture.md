---
title: 架构设计
---

# 架构设计

本文从高层视角介绍 Trojan-Go 的系统设计和数据流。

## 核心设计原则

Trojan-Go 的核心设计是**可插拔隧道栈**（Pluggable Tunnel Stack）。每个功能模块（TLS、WebSocket、Trojan 协议、路由等）都实现为独立的隧道层，通过组合不同的隧道层构成完整的代理功能。

```
┌─────────────────────────────────────────┐
│              代理模式 (Proxy)             │
│  client / server / forward / nat / custom │
├─────────────────────────────────────────┤
│              隧道栈 (Tunnel Stack)       │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐   │
│  │入站层│→│协议层│→│加密层│→│传输层│   │
│  └──────┘ └──────┘ └──────┘ └──────┘   │
├─────────────────────────────────────────┤
│           基础设施 (Infrastructure)      │
│  config / log / metric / statistic      │
└─────────────────────────────────────────┘
```

## 五种代理模式

每种模式是不同的隧道栈组合：

| 模式 | 入站 | 出站 | 用途 |
|------|------|------|------|
| CLIENT | socks+http | 隧道栈 | 标准客户端 |
| SERVER | tls/ws | freedom/router | 标准服务端 |
| FORWARD | dokodemo | 隧道栈 | 端口转发 |
| NAT | tproxy | 隧道栈 | 透明代理 |
| CUSTOM | 自定义 | 自定义 | 完全控制 |

## 客户端数据流（示例）

一个典型的客户端连接（启用了 mux + shadowsocks + websocket）：

```
应用程序
   │
   ▼
┌─────────┐   SOCKS5/HTTP
│ adapter  │ ──────────────→
└─────────┘
   │
   ▼
┌─────────┐   Trojan 协议封装
│ trojan   │ ──────────────→
└─────────┘
   │
   ▼
┌─────────┐   smux 多路复用
│ mux      │ ──────────────→
└─────────┘
   │
   ▼
┌─────────┐   Shadowsocks AEAD
│ shadowsocks│ ─────────────→
└─────────┘
   │
   ▼
┌─────────┐   TLS 加密
│ tls      │ ──────────────→
└─────────┘
   │
   ▼
┌─────────┐   WebSocket 帧
│ websocket│ ──────────────→
└─────────┘
   │
   ▼
┌─────────┐   TCP 传输
│ transport│ ──────────────→  远端服务器
└─────────┘
```

## 服务端数据流

服务端使用**分支树**结构，同时支持直接 TLS 和 WebSocket 两种接入方式：

```
                    TLS 监听 (443)
                        │
              ┌─────────┴──────────┐
              │                     │
        直接 TLS 连接          WebSocket 连接
              │                     │
              ▼                     ▼
         TLS 解密            WS + TLS 解密
              │                     │
              └─────────┬──────────┘
                        │
                  Trojan 协议解析
                  /    │    \
              密码错误  密码正确  非Trojan
                  │      │       │
              回退到    路由     代理到
             HTTP服务  freedom  remote_addr
```

## 配置系统

采用基于 context 的依赖注入：

```go
// 注册配置创建器
config.RegisterConfigCreator("mysql", func(ctx context.Context) ...

// 从 context 获取配置
cfg := config.FromContext(ctx, "mysql")
```

每个包通过 `init()` 自注册，无需手动导入。

## 构建标签系统

`component/` 目录通过 Go build tags 控制功能模块：

```go
// +build full
// component/client.go — 全量构建包含客户端

// +build !full,!mini,!client
// 这个文件不会编译（排除条件）
```

这允许构建不同大小的二进制文件，从完整的全功能版本到仅客户端的精简版。

## 关键文件索引

| 功能 | 目录 |
|------|------|
| 代理模式 | `proxy/` |
| 隧道层 | `tunnel/` |
| API | `api/` |
| 用户认证 | `statistic/` |
| 路由 | `tunnel/router/` |
| TLS | `tunnel/tls/` |
| WebSocket | `tunnel/websocket/` |
| 配置 | `config/` |
| 日志 | `log/` |
| 指标 | `metric/` |
