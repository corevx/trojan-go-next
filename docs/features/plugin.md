---
title: "可插拔传输层"
---

# 可插拔传输层

::: warning
Trojan 原版不支持此特性。此功能为 Trojan-Go-Next 扩展。
:::

## 功能概述

Trojan-Go-Next 支持可插拔的传输层插件，可以替换默认的 TLS 传输层。开启插件后，Trojan-Go-Next 客户端会将**未经 TLS 加密的明文流量**交给本地插件处理，由插件负责加密、混淆和传输；服务端插件接收流量后解密，再将明文流量交给本地 Trojan-Go-Next 服务端。

```
+-----------+   明文   +-------------+   加密传输   +-------------+   明文   +-----------+
| Trojan-Go-Next | ------> | 客户端插件   | ----------> | 服务端插件   | ------> | Trojan-Go-Next |
| 客户端     |         | (加密/混淆)  |             | (解密/还原)  |         | 服务端     |
+-----------+         +-------------+             +-------------+         +-----------+
```

这一机制意味着你可以使用任何具备 TCP 隧道能力的软件作为传输层，实现自定义的流量伪装和加密策略。

## 适用场景

- 默认 TLS 传输特征被识别，需要额外的流量伪装
- 需要将 Trojan 流量伪装为特定协议（如 HTTP/WebSocket）
- 在特定网络环境下需要使用自定义传输协议

::: danger
开启可插拔传输层后，Trojan-Go-Next 默认的 TLS 加密将被**完全替换**。如果插件本身不提供加密，流量将以明文传输。请务必选择具备加密能力的插件，否则你的代理密码和流量内容将完全暴露。
:::

## 插件类型对比

| 类型 | 说明 | SIP003 兼容 | 加密能力 | 安全性 |
|------|------|-------------|----------|--------|
| `shadowsocks` | SIP003 标准插件，如 v2ray-plugin、GoQuiet | 是 | 取决于插件 | 取决于插件 |
| `plaintext` | 明文 TCP 传输，不启动任何插件 | 否 | 无 | 极低 |
| `other` | 自定义插件，需手动配置参数和环境变量 | 否 | 取决于插件 | 取决于插件 |

::: warning 关于 plaintext 类型
`plaintext` 类型会**完全移除 TLS 传输层**，所有 Trojan 协议流量以明文 TCP 传输。此模式仅适用于以下场景：
- 使用 nginx 等反向代理接管 TLS 并进行路径分发
- 本地调试和测试

**绝对不要使用 plaintext 模式直接连接互联网，更不要用于穿越防火墙。**
:::

## 配置示例

### 使用 v2ray-plugin（推荐）

v2ray-plugin 是一个符合 SIP003 标准的插件，支持将流量伪装为 WebSocket 并提供域名伪装能力。

首先下载 [v2ray-plugin](https://github.com/shadowsocks/v2ray-plugin) 并放置在 Trojan-Go-Next 同目录下。

**服务端配置**：

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
        "cert": "/path/to/your-cert.pem",
        "key": "/path/to/your-key.pem"
    },
    "transport_plugin": {
        "enabled": true,
        "type": "shadowsocks",
        "command": "./v2ray-plugin",
        "arg": ["-server", "-host", "www.example.com"]
    }
}
```

**客户端配置**：

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "your-server-domain.com",
    "remote_port": 443,
    "password": [
        "your-strong-password"
    ],
    "transport_plugin": {
        "enabled": true,
        "type": "shadowsocks",
        "command": "./v2ray-plugin",
        "arg": ["-host", "www.example.com"]
    }
}
```

::: tip
v2ray-plugin 需要使用 `-server` 参数来区分服务端和客户端。服务端配置中必须包含 `-server`，客户端不要添加。更多参数说明请参考 v2ray-plugin 的项目文档。
:::

### 使用自定义插件（other 类型）

对于非 SIP003 标准的插件，使用 `other` 类型并手动指定参数和环境变量：

```json
{
    "transport_plugin": {
        "enabled": true,
        "type": "other",
        "command": "/path/to/your-plugin",
        "arg": ["-config", "/path/to/plugin-config.json"],
        "env": ["PLUGIN_KEY=your-plugin-secret"]
    }
}
```

## SIP003 兼容性

Trojan-Go-Next 的 `shadowsocks` 插件类型完全兼容 SIP003 标准。启用后，Trojan-Go-Next 会按照 SIP003 规范设置环境变量（如 `SS_LOCAL_HOST`、`SS_REMOTE_HOST` 等），并自动调整 `remote_addr`/`remote_port`/`local_addr`/`local_port`，使插件直接与远端通讯，Trojan-Go-Next 仅与本地插件交互。

## 已验证可用的插件

以下插件经过社区验证，可与 Trojan-Go-Next 配合使用：

| 插件 | 说明 | 加密 | 备注 |
|------|------|------|------|
| [v2ray-plugin](https://github.com/shadowsocks/v2ray-plugin) | WebSocket 传输 + 域名伪装 | 可选 TLS | 最常用的 SIP003 插件 |
| [GoQuiet](https://github.com/cbeuw/GoQuiet) | 基于域名探测的流量伪装 | 有 | 需配置服务端探针页面 |
| [Cloak](https://github.com/cbeuw/Cloak) | 域名前置 + 流量伪装 | 有 | 功能较全面 |
| [obfs4](https://gitlab.com/yawning/obfs4) | Tor 传输层插件 | 有 | 来自 Tor 项目 |

::: warning
目前现有的插件均无法对接 Trojan-Go-Next 的主动探测防御特性。如果你对安全性有较高要求，建议自行设计协议并开发相应插件。开发指南请参考"实现细节和开发指南"章节。
:::

## 验证方法

```bash
# 1. 启动 Trojan-Go-Next，观察插件是否正常启动
trojan-go-next -config config.json
# 日志中应显示插件的启动输出

# 2. 测试代理连通性
curl -x socks5://127.0.0.1:1080 https://www.google.com

# 3. 如果使用 v2ray-plugin，可以用抓包工具确认流量特征
# 确认外层为 WebSocket 而非原始 Trojan 协议
```
