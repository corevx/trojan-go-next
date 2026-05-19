---
title: "隧道与反向代理"
---

# 隧道与反向代理

## 功能概述

FORWARD 模式是 Trojan-Go 的端口转发功能。它将本地端口的 TCP/UDP 流量通过 Trojan TLS 隧道传输到远端服务器，再由服务器将流量转发到指定的目标地址和端口。

本质上，FORWARD 模式是一个特殊的客户端——它不提供 SOCKS5/HTTP 代理接口，而是直接在本地监听一个端口，将所有进入该端口的流量原封不动地通过隧道转发到远端目标。

```
+----------+       Trojan TLS 隧道        +------------+
| 本地应用  |  -->  本地:port  ──────>  远端服务器  -->  target:port |
+----------+       (加密传输)             +------------+
```

## 适用场景

### DNS 隧道

在本地搭建一个无污染的 DNS 服务器。所有 DNS 查询通过 Trojan 隧道发送到远端服务器，由远端服务器向真实 DNS（如 8.8.8.8）发起请求并返回结果，从而避开 DNS 污染。

```json
{
    "run_type": "forward",
    "local_addr": "127.0.0.1",
    "local_port": 53,
    "remote_addr": "your-server-domain.com",
    "remote_port": 443,
    "target_addr": "8.8.8.8",
    "target_port": 53,
    "password": [
        "your-strong-password"
    ]
}
```

启动后，将系统 DNS 设置为 `127.0.0.1` 即可获得无污染的 DNS 解析结果。

### 访问内网服务

通过 Trojan 隧道访问远端服务器所在内网的服务。例如，远端服务器所在网络有一台内网数据库 `10.0.0.5:3306`，你可以在本地通过转发端口安全访问。

```json
{
    "run_type": "forward",
    "local_addr": "127.0.0.1",
    "local_port": 13306,
    "remote_addr": "your-server-domain.com",
    "remote_port": 443,
    "target_addr": "10.0.0.5",
    "target_port": 3306,
    "password": [
        "your-strong-password"
    ]
}
```

访问 `127.0.0.1:13306` 等同于直接连接远端内网的 `10.0.0.5:3306`。

### Shadowsocks-over-Trojan

将其他代理协议的流量通过 Trojan 隧道传输。例如，远端服务器运行了一个 Shadowsocks 服务端监听 `127.0.0.1:12345`，同时运行了 Trojan-Go 服务端监听 443 端口：

```json
{
    "run_type": "forward",
    "local_addr": "0.0.0.0",
    "local_port": 54321,
    "remote_addr": "your-server-domain.com",
    "remote_port": 443,
    "target_addr": "127.0.0.1",
    "target_port": 12345,
    "password": [
        "your-strong-password"
    ]
}
```

此后，使用 Shadowsocks 客户端连接本机 `54321` 端口，SS 流量将通过 Trojan 隧道加密传输到远端 SS 服务器。

::: tip
`target_addr` 使用 `127.0.0.1` 时，指的是远端服务器的本地回环地址，而非你本机的地址。
:::

## 配置说明

FORWARD 模式的核心配置项：

| 字段 | 说明 | 必填 |
|------|------|------|
| `run_type` | 固定填写 `"forward"` | 是 |
| `local_addr` | 本地监听地址，如 `"127.0.0.1"` 或 `"0.0.0.0"` | 是 |
| `local_port` | 本地监听端口 | 是 |
| `remote_addr` | Trojan 服务器地址 | 是 |
| `remote_port` | Trojan 服务器端口 | 是 |
| `target_addr` | 转发目标地址（远端服务器视角） | 是 |
| `target_port` | 转发目标端口 | 是 |
| `password` | Trojan 服务端密码列表 | 是 |

`target_addr` 和 `target_port` 是 FORWARD 模式独有的字段，指定远端服务器收到流量后转发到的目标地址。该地址以远端服务器的网络视角解析——填 `127.0.0.1` 表示远端服务器本机，填内网 IP 则表示远端服务器可达的内网地址。

`local_addr` 设为 `127.0.0.1` 仅允许本机访问，设为 `0.0.0.0` 则允许局域网内其他设备访问转发的端口。

## 验证方法

以 DNS 转发为例：

```bash
# 1. 启动 Trojan-Go FORWARD 模式
trojan-go -config config.json

# 2. 使用 dig 测试 DNS 查询（指定本地转发的 53 端口）
dig @127.0.0.1 -p 53 google.com

# 3. 对比：使用本地 DNS 直接查询（可能被污染）
dig @127.0.0.1 -p 53 google.com
# 观察返回的 IP 是否为真实 Google IP
```

对于通用端口转发，可以使用 `nc` 或 `curl` 验证连通性：

```bash
# 测试 TCP 转发是否正常
nc -zv 127.0.0.1 <local_port>

# 测试 HTTP 服务转发
curl -v http://127.0.0.1:<local_port>/
```
