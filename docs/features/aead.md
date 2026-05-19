---
title: "使用Shadowsocks AEAD进行二次加密"
---

# 使用 Shadowsocks AEAD 进行二次加密

## 功能概述

Trojan-Go-Next 支持在 Trojan 协议之下叠加一层 Shadowsocks AEAD 加密。数据流经路径为：

```
应用数据 → Trojan 协议 → Shadowsocks AEAD 加密 → TLS 隧道 → 网络
```

Trojan 协议本身无加密，安全性完全依赖下层 TLS。在 TLS 安全可保障的情况下，无需启用此功能。但当 TLS 隧道的安全性无法保证时，AEAD 二次加密可提供额外的保护层。

## 适用场景

| 场景 | 说明 | 是否建议启用 |
|------|------|-------------|
| WebSocket 经由不可信 CDN | 国内 CDN 可窥探甚至修改 TLS 终止后的明文流量 | 强烈建议 |
| TLS 中间人攻击 | GFW 对 TLS 连接进行主动中间人干扰 | 建议 |
| 证书失效 | 无法验证服务端证书有效性时 | 建议 |
| 不安全的传输层 | 使用了无法保证密码学安全的自定义传输 | 建议 |
| 标准 Trojan 直连 | 客户端直接连接服务端，TLS 证书有效 | 不需要 |

::: tip
如果你通过 WebSocket + CDN 中转流量，建议同时启用 AEAD 加密。详见 [WebSocket 传输](./websocket.md)。
:::

## 配置方法

::: warning
服务端和客户端**必须同时启用** AEAD 加密，且密码和加密方式必须完全一致，否则无法通讯。
:::

在客户端和服务端配置中添加 `shadowsocks` 选项：

**客户端配置：**

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
    "shadowsocks": {
        "enabled": true,
        "method": "AES-128-GCM",
        "password": "another-strong-password"
    }
}
```

**服务端配置：**

```json
{
    "run_type": "server",
    "local_addr": "0.0.0.0",
    "local_port": 443,
    "password": [
        "your-strong-password"
    ],
    "shadowsocks": {
        "enabled": true,
        "method": "AES-128-GCM",
        "password": "another-strong-password"
    }
}
```

### 配置项说明

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用 Shadowsocks AEAD 加密 |
| `method` | string | `AES-128-GCM` | 加密方式 |
| `password` | string | - | AEAD 加密密码，需与服务端一致 |

### 支持的加密方式

| 加密方式 | 说明 |
|---------|------|
| `AES-128-GCM` | 默认方式，性能与安全性均衡 |
| `AES-256-GCM` | 更高安全强度，性能略低于 AES-128 |
| `CHACHA20-IETF-POLY1305` | 在不支持 AES 硬件加速的设备上性能更优 |

::: tip
`method` 可以省略，默认使用 `AES-128-GCM`。在 ARM 设备或老旧 CPU 上，`CHACHA20-IETF-POLY1305` 通常性能更好。
:::

## 验证方法

启动客户端时观察日志输出。启用 AEAD 加密后，日志中应出现 Shadowsocks 层的信息：

```bash
# 以 debug 模式启动客户端
trojan-go-next -config config.json -log debug
```

在日志中查找隧道栈信息，确认包含 `shadowsocks` 层：

```
[DEBUG] tunnel stack: socks->adapter->trojan->shadowsocks->tls->transport
```

如果连接正常建立且无解密错误，说明 AEAD 加密工作正常。若服务端和客户端密码不匹配，日志中会出现解密失败的错误信息。

## 性能影响

AEAD 加密会增加 CPU 开销和少量流量（加密头和认证标签）。在大多数现代设备上，AES-GCM 的硬件加速使性能影响可以忽略。仅在低性能设备上需要注意 CPU 占用。
