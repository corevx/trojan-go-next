---
title: 隧道 API 参考
---

# 隧道 API 参考

Trojan-Go 的每个功能模块都实现为隧道层（Tunnel）。本文介绍隧道接口的设计和如何开发自定义隧道。

## 核心接口

定义在 `tunnel/tunnel.go`：

```go
// Tunnel 是所有隧道层的基接口
type Tunnel interface {
    // 注册隧道到全局注册表
    // 通常在 init() 中调用
}

// Server 是入站隧道的服务端
type Server interface {
    // Accept 接受入站连接
    AcceptConn() (Conn, error)
    AcceptPacket() (PacketConn, error)
    Close() error
}

// Client 是出站隧道的客户端
type Client interface {
    // Dial 建立到目标的连接
    Dial(*Address) (Conn, error)
    DialPacket(*Address) (PacketConn, error)
    Close() error
}
```

## 注册机制

每个隧道层通过 `init()` 注册自身：

```go
// tunnel/mux/mux.go
func init() {
    tunnel.Register(tunnel.TCP, tunnel.TCP, "mux", func(ctx context.Context) tunnel.Tunnel {
        return &MuxTunnel{}
    })
}
```

注册参数：

| 参数 | 说明 |
|------|------|
| 下层类型 | 此隧道消费的下层协议（如 TCP） |
| 上层类型 | 此隧道提供的上层协议（如 TCP） |
| 名称 | 隧道标识符 |
| 工厂函数 | 创建隧道实例 |

## 隧道组合

隧道通过组合形成栈。例如客户端的典型组合：

```
transport(TCP) → tls(TCP) → trojan(TCP)
```

每个隧道消费下层提供的服务，向上层提供自己的服务。

## 隧道类型

### 按数据类型

| 类型 | 接口 | 说明 |
|------|------|------|
| 流式 | `Conn` | TCP 连接，有序字节流 |
| 数据包 | `PacketConn` | UDP 数据包，无连接 |

### 按角色

| 角色 | 接口 | 说明 |
|------|------|------|
| 提供者 | `Server` / `Client` | 向上层提供连接 |
| 消费者 | 通过注入下层 | 使用下层的连接 |

## 所有内置隧道

| 隧道 | 下层 | 上层 | 说明 |
|------|------|------|------|
| transport | 无（最底层） | TCP/UDP | 原始传输 |
| tls | TCP | TCP | TLS 加密 |
| websocket | TCP | TCP | WebSocket 传输 |
| trojan | TCP | TCP | Trojan 协议 |
| shadowsocks | TCP | TCP | AEAD 加密 |
| mux | TCP | TCP | smux 多路复用 |
| simplesocks | TCP | TCP | 简化 SOCKS（mux 内部） |
| adapter | 无 | TCP | SOCKS5/HTTP 入站 |
| socks | 无 | TCP | SOCKS5 入站 |
| http | 无 | TCP | HTTP CONNECT 入站 |
| dokodemo | 无 | TCP | 任意地址入站 |
| tproxy | 无 | TCP/UDP | 透明代理入站 |
| router | TCP | TCP | 路由分流 |
| freedom | TCP | 无（最顶层） | 直连出站 |

## 开发自定义隧道

1. 实现 `tunnel.Tunnel` 接口
2. 在 `init()` 中注册
3. 在 `component/` 中添加构建标签导入

参见 [传输层插件开发](/developer/plugin-dev) 了解详细开发指南。
