---
title: "使用多路复用提升并发性能"
---

# 使用多路复用提升并发性能

## 功能概述

Trojan-Go-Next 基于 smux 协议实现了连接多路复用。其核心思路是在一条 TLS 隧道内同时承载多条 TCP 连接，避免每次代理请求都经历完整的 TLS 握手和 TCP 三次握手。

**工作原理：** 客户端与服务端建立 TLS 连接后，在该连接上运行 smux 会话。新的代理请求复用已有的 TLS 连接，而非重新握手。对上层应用而言，每个连接依然独立，但底层共享同一条加密通道。

## 适用场景

多路复用**降低延迟**，而非提升吞吐。适合以下情况：

- 浏览包含大量图片、CSS、JS 的网页（浏览器并发请求通常为 6-12 个）
- 发送大量 UDP 请求（如实时语音/视频）
- 线路 TLS 握手耗时较长（高延迟或拥塞网络）

::: warning
多路复用不会提升、甚至可能**略微降低**单连接的吞吐量。如果你主要关注大文件下载速度，不建议启用此功能。
:::

## 配置方法

多路复用为**客户端功能**，服务端自动检测并适配，无需任何配置。

在客户端配置中添加 `mux` 选项：

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "your-server.com",
    "remote_port": 443,
    "password": [
        "your-strong-password"
    ],
    "mux": {
        "enabled": true
    }
}
```

### 完整配置项

```json
"mux": {
    "enabled": false,
    "concurrency": 8,
    "idle_timeout": 30
}
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用多路复用 |
| `concurrency` | int | `8` | 每条 TLS 连接承载的最大 TCP 连接数 |
| `idle_timeout` | int | `30` | TLS 连接空闲超时时间（秒） |

### 参数调优

**concurrency（并发度）**

- 默认值 `8` 适合大多数浏览场景
- 增大该值可提升单条 TLS 连接的复用程度，进一步降低握手延迟，但会增加 CPU 开销
- 设为 `-1` 时，客户端仅建立一条 TLS 连接，所有流量都通过该连接传输。适合 TLS 握手极端缓慢的线路

::: tip
`concurrency: -1` 模式下，唯一的 TLS 连接断开后所有代理请求将中断，可靠性不如多连接模式。仅在握手成本远高于可靠性需求时使用。
:::

**idle_timeout（空闲超时）**

- 默认 `30` 秒，空闲 30 秒后关闭 TLS 连接
- 适当缩短超时可能有助于减少 Keep-Alive 流量，降低被探测的风险
- 设为 `-1` 时，TLS 连接空闲后立即关闭

## 验证方法

使用 `curl` 的计时功能对比启用前后的连接延迟：

```bash
# 不启用 mux
curl -x socks5://127.0.0.1:1080 \
     -o /dev/null -s \
     -w "DNS: %{time_namelookup}s\nTCP: %{time_connect}s\nTLS: %{time_appconnect}s\nTotal: %{time_total}s\n" \
     https://www.example.com

# 启用 mux 后重复请求（第二次起复用连接）
curl -x socks5://127.0.0.1:1080 \
     -o /dev/null -s \
     -w "DNS: %{time_namelookup}s\nTCP: %{time_connect}s\nTLS: %{time_appconnect}s\nTotal: %{time_total}s\n" \
     https://www.example.com
```

启用多路复用后，第二次及后续请求的 TLS 握手时间（`time_appconnect`）应接近零，总体延迟明显降低。

## 何时不应使用

- **大文件传输 / 测速场景：** 多路复用的分帧开销会降低吞吐
- **低并发使用：** 偶尔浏览少量页面，握手开销本身不大
- **不稳定网络：** 单条 TLS 连接承载多个流，连接断开影响范围更大
