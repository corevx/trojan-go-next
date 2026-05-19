---
title: "使用Websocket进行CDN转发和抵抗中间人攻击"
---

# 使用 WebSocket 进行 CDN 转发

::: warning
Trojan 原版不支持此特性。此功能为 Trojan-Go 扩展。
:::

## 功能概述

Trojan-Go 支持在 TLS 传输层之上使用 WebSocket 承载 Trojan 协议。这使得利用 CDN（如 Cloudflare）进行流量中转成为可能——客户端连接到 CDN 节点，CDN 再将 WebSocket 流量回源到你的 Trojan-Go 服务器。

```
+-----------+     TLS      +---------+     回源      +------------+
| Trojan-Go | ----------> |   CDN   | ----------> | Trojan-Go  |
| 客户端     |   (WS over  | (如 CF) |   (WS over  | 服务端      |
|           |    TLS)      |         |    TLS)     |            |
+-----------+             +---------+             +------------+
```

在正常的直连代理场景下，开启 WebSocket 不会提升速度或安全性。WebSocket 传输仅适用于以下场景：

- 需要通过 CDN 中转隐藏真实服务器 IP
- 利用 CDN 的 Anycast 网络改善连接质量
- 使用 nginx 等反向代理按路径分流
- IP 被封锁，需要通过 CDN 域名接入

## CDN 安全警告

::: danger
Trojan 协议本身不携带加密，安全性完全依赖外层的 TLS 加密。但流量一旦经过 CDN，TLS 对 CDN 服务商是**完全透明**的。CDN 服务商可以审查 TLS 明文内容，识别并记录你的代理行为。

**如果你使用的是不可信的 CDN（任何在中国大陆注册备案的 CDN 服务均应被视为不可信），必须开启 Shadowsocks AEAD 加密。**
:::

开启 Shadowsocks AEAD 后，即使在不可信 CDN 上，代理流量也会被额外加密，CDN 无法识别流量内容：

```json
"shadowsocks": {
    "enabled": true,
    "method": "AES-128-GCM",
    "password": "your-strong-aead-password"
}
```

## 配置说明

服务端和客户端需要同时配置 WebSocket 选项，且 `path` 必须一致。

### 服务端配置

```json
{
    "run_type": "server",
    "local_addr": "0.0.0.0",
    "local_port": 443,
    "remote_addr": "127.0.0.1",
    "remote_port": 80,
    "password": [
        "your-strong-password"
    ],
    "ssl": {
        "cert": "/etc/letsencrypt/live/your-domain.com/fullchain.pem",
        "key": "/etc/letsencrypt/live/your-domain.com/privkey.pem"
    },
    "websocket": {
        "enabled": true,
        "path": "/your-secret-websocket-path",
        "host": "your-domain.com"
    },
    "shadowsocks": {
        "enabled": true,
        "method": "AES-128-GCM",
        "password": "your-strong-aead-password"
    }
}
```

### 客户端配置

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "your-domain.com",
    "remote_port": 443,
    "password": [
        "your-strong-password"
    ],
    "websocket": {
        "enabled": true,
        "path": "/your-secret-websocket-path",
        "host": "your-domain.com"
    },
    "shadowsocks": {
        "enabled": true,
        "method": "AES-128-GCM",
        "password": "your-strong-aead-password"
    }
}
```

### 字段说明

| 字段 | 说明 |
|------|------|
| `websocket.enabled` | 启用 WebSocket 传输。服务端开启后同时支持原始 Trojan 协议和 WebSocket Trojan 协议；客户端开启后仅使用 WebSocket 传输 |
| `websocket.path` | WebSocket 的 URL 路径，必须以 `/` 开头，服务端与客户端必须一致 |
| `websocket.host` | WebSocket 握手 HTTP 请求中的主机名。客户端留空时使用 `remote_addr` 填充。使用 CDN 时必须填写域名 |

::: warning 关于 path 的安全性
`path` 应使用足够长且随机的字符串，例如 `/dG9yb2phbi1nby13cy1wYXRo`。过短或常见的路径（如 `/ws`、`/websocket`）容易被主动探测发现。**服务端与客户端的 `path` 必须完全一致，否则握手无法完成。**
:::

## Cloudflare 配置步骤

以下以 Cloudflare 为例，说明如何配置 CDN 中转。

### 第 1 步：DNS 设置

1. 登录 Cloudflare 控制台，进入你的域名管理页面
2. 在 DNS 记录中添加一条 A 记录：
   - 名称：`your-subdomain`（或使用根域名 `@`）
   - IPv4 地址：你的 Trojan-Go 服务器 IP
   - 代理状态：**开启**（橙色云朵图标，表示流量经过 Cloudflare）

### 第 2 步：TLS 模式

1. 进入 SSL/TLS 设置页面
2. 将加密模式设为 **Full**（完全）
   - **不要使用 Full (Strict)**，因为 Cloudflare 到你服务器的回源连接使用的是 Cloudflare 自己信任的证书链
   - **不要使用 Flexible**，否则 Cloudflare 会以 HTTP 回源，WebSocket 连接将失败

### 第 3 步：WebSocket 配置

1. Trojan-Go 服务端和客户端配置中同时启用 `websocket` 选项
2. 填写 `path`（使用随机长字符串）和 `host`（填写 Cloudflare 上的域名）
3. 务必同时启用 `shadowsocks` AEAD 加密

::: tip
Cloudflare 免费计划即支持 WebSocket，无需升级付费计划。但免费计划的回源端口有限制，默认仅支持 443 端口。
:::

## 验证方法

```bash
# 1. 启动 Trojan-Go 服务端
trojan-go -config server-config.json

# 2. 启动 Trojan-Go 客户端
trojan-go -config client-config.json

# 3. 测试代理连通性
curl -x socks5://127.0.0.1:1080 https://www.google.com -v

# 4. 确认经过 CDN
# 客户端日志中应显示连接到 CDN 节点 IP（而非你的服务器真实 IP）
# 可以通过响应头中的 cf-ray 字段确认流量经过了 Cloudflare
curl -x socks5://127.0.0.1:1080 https://www.google.com -s -o /dev/null -D - | grep -i cf-ray

# 5. 服务端日志验证
# 服务端日志中应出现 WebSocket 连接记录
# 如果看不到任何连接记录，检查 path 是否一致、CDN TLS 模式是否为 Full
```
