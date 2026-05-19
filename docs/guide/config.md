---
title: "正确配置Trojan-Go"
---

本页介绍 Trojan-Go 配置文件的整体结构与核心概念。如果你需要查看所有字段的完整说明，请参考 [完整配置文件](./full-config.md)。

## 配置格式：JSON 与 YAML

Trojan-Go 同时支持 JSON 和 YAML 两种配置格式，二者结构等价。YAML 格式中字段名使用横杠（`-`）替代下划线（`_`），例如 `remote_addr` 写作 `remote-addr`。

下面是同一份最小化服务端配置的两种写法：

:::: code-group
::: code-group-item JSON

```json
{
  "run_type": "server",
  "local_addr": "0.0.0.0",
  "local_port": 443,
  "remote_addr": "127.0.0.1",
  "remote_port": 80,
  "password": ["s3cret"],
  "ssl": {
    "cert": "server.crt",
    "key": "server.key"
  }
}
```

:::
::: code-group-item YAML

```yaml
run-type: server
local-addr: 0.0.0.0
local-port: 443
remote-addr: 127.0.0.1
remote-port: 80
password:
  - s3cret
ssl:
  cert: server.crt
  key: server.key
```

:::
::::

启动时通过文件扩展名自动识别格式：`.json` 按 JSON 解析，`.yaml` / `.yml` 按 YAML 解析。

## 五种运行模式

Trojan-Go 通过 `run_type` 字段决定工作模式。不同模式的含义和数据流向差异较大：

| `run_type` | 说明 | 入站（接收流量） | 出站（转发流量） |
| ---------- | ---- | ---------------- | ---------------- |
| `client`   | 标准客户端，在本地开启 SOCKS5/HTTP 代理 | 本地 `local_addr:local_port` | 经 TLS 隧道发往远端服务器 |
| `server`   | 标准服务端，监听外部连接并代理 | TLS 监听 `local_addr:local_port` | 转发至 `freedom`（直连）或 `router` |
| `forward`  | 端口转发，将本地端口流量经隧道转至远端 | 任意地址 `local_addr:local_port` | 经 TLS 隧道发往远端 |
| `nat`      | 透明代理（仅 Linux），配合 TProxy 使用 | TProxy 捕获的流量 | 经 TLS 隧道发往远端 |
| `custom`   | 自定义隧道栈，由用户自行组合 | 用户定义 | 用户定义 |

## 配置继承与默认值

配置项分为 **必填** 和 **可选** 两类。未填写的可选项会使用内置默认值。

**必填字段**（所有模式通用）：

- `run_type` — 运行模式
- `local_addr` / `local_port` — 本地监听地址
- `remote_addr` / `remote_port` — 远端地址

**模式相关必填**：

- `server` 模式：`ssl.cert` 和 `ssl.key`（TLS 证书与私钥）
- `client` / `forward` / `nat` 模式：`password`（认证密码）

**可选字段**的默认行为举例：

| 字段 | 默认值 | 说明 |
| ---- | ------ | ---- |
| `mux.enabled` | `false` | 多路复用默认关闭 |
| `websocket.enabled` | `false` | WebSocket 传输默认关闭 |
| `shadowsocks.enabled` | `false` | Shadowsocks AEAD 加密默认关闭 |
| `router.enabled` | `false` | 路由分流默认关闭 |
| `ssl.verify` | `true` | 客户端默认验证服务端证书 |
| `ssl.sni` | 取 `remote_addr` 值 | 客户端未指定时自动填充 |

## 核心概念

### `remote_addr` / `remote_port` —— 双重含义

这两个字段在客户端和服务端扮演不同角色：

- **客户端（client / forward / nat）**：指向 Trojan 服务端的地址和端口，即 TLS 隧道的终点。
- **服务端（server）**：指向 HTTP 回落地址。当 TLS 握手成功但流量被识别为非 Trojan 协议时（例如浏览器访问、GFW 主动探测），Trojan-Go 会将连接转发到该地址，使服务器表现与正常 HTTPS 网站一致。

### `local_addr` / `local_port`

- **客户端**：本地 SOCKS5/HTTP 代理的监听地址，通常为 `127.0.0.1:1080`。
- **服务端**：TLS 监听地址，对外提供服务，通常为 `0.0.0.0:443`。

### `password` —— 支持多用户

`password` 字段接受数组格式，可配置多个密码，每个密码对应一个用户。服务端和客户端的密码必须一致才能通过认证。

```json
"password": [
  "password_for_user_alice",
  "password_for_user_bob"
]
```

除配置文件外，还支持通过 MySQL 数据库或 gRPC API 动态管理用户。

### `ssl.sni` —— TLS 握手中的明文字段

SNI（Server Name Indication）在 TLS Client Hello 中**以明文传输**，用于告知服务器应返回哪张证书。

- 如果 `remote_addr` 填写的是域名，`sni` 可省略（自动使用该域名）。
- 如果 `remote_addr` 填写的是 IP 地址，**必须手动指定 `sni`** 为证书对应的域名。
- **切勿填写已被封锁的域名**（如 `google.com`），GFW 具备 SNI 探测和阻断能力。

## 启动方式

Trojan-Go 支持三种启动方式：

### 1. 配置文件模式

最常用的方式，通过 `-config` 指定配置文件路径：

```bash
trojan-go -config ./client.json
```

支持同时指定多个配置文件，后者会覆盖前者的同名字段：

```bash
trojan-go -config ./base.json -config ./override.json
```

### 2. 简易模式（Easy Mode）

无需配置文件，通过命令行参数快速启动：

```bash
# 客户端
trojan-go -client -remote your-server.com:443 -local 127.0.0.1:1080 -password s3cret

# 服务端
trojan-go -server -local 0.0.0.0:443 -remote 127.0.0.1:80 -password s3cert -cert server.crt -key server.key
```

简易模式适合快速测试和临时使用。

### 3. URL 模式

通过 Trojan-Go URI 启动，适合从分享链接直接连接：

```bash
trojan-go 'trojan-go://s3cret@your-server.com:443?sni=your-server.com#my-server'
```

URI 格式遵循 `trojan-go://password@host:port?parameters#name` 的结构。

## 下一步

- 查看 [完整配置文件](./full-config.md) 了解所有字段的详细说明
- 了解 [WebSocket 传输](/features/websocket)、[多路复用](/features/mux) 等扩展功能
