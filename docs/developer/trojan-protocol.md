---
title: "Trojan 协议规范"
---

# Trojan 协议规范

Trojan-Go-Next 实现了原始 Trojan 协议，并在此基础上扩展了多路复用（Mux）命令。本文档描述协议的完整格式和行为。

## 协议栈

Trojan 协议本身不提供加密，依赖传输层（通常是 TLS）保证安全性。协议栈自上而下为：

```
+---------------------------+
|      真实应用流量          |  HTTP / 任意 TCP / UDP
+---------------------------+
|     Trojan 协议            |  密码认证 + 目标地址
+---------------------------+
|     TLS / WebSocket        |  加密层（或自定义传输层插件）
+---------------------------+
|     TCP                    |  底层传输
+---------------------------+
```

## 请求格式

### TCP 连接请求

客户端通过 TLS 连接发送的第一条消息为 Trojan 请求头：

```
+---------------------+
| Password Hash (56B) |   SHA224(password)，十六进制字符串
+---------------------+
| CRLF                |   0x0D 0x0A
+---------------------+
| CMD (1 byte)        |   连接命令
+---------------------+
| ATYP (1 byte)       |   地址类型
+---------------------+
| DST.ADDR (variable) |   目标地址
+---------------------+
| DST.PORT (2 bytes)  |   目标端口，大端序
+---------------------+
| CRLF                |   0x0D 0x0A
+---------------------+
| Payload             |   首包数据（可为空）
+---------------------+
```

::: warning 注意
Trojan-Go-Next 的代码实现中，请求头的顺序是：`Hash + CRLF + CMD + ATYP + DST.ADDR + DST.PORT + CRLF + Payload`。密码哈希在前，命令和地址在后。这与某些文档描述的顺序不同，以代码实现为准。
:::

源码参考 `tunnel/trojan/client.go` 中 `OutboundConn.WriteHeader`：

```go
func (c *OutboundConn) WriteHeader(payload []byte) (bool, error) {
    var err error
    written := false
    c.headerWrittenOnce.Do(func() {
        hash := c.user.Hash()           // 56 字节 SHA224 十六进制字符串
        buf := bytes.NewBuffer(make([]byte, 0, MaxPacketSize))
        crlf := []byte{0x0d, 0x0a}
        buf.Write([]byte(hash))         // Password Hash
        buf.Write(crlf)                 // CRLF
        c.metadata.WriteTo(buf)         // CMD + ATYP + DST.ADDR + DST.PORT
        buf.Write(crlf)                 // CRLF
        if payload != nil {
            buf.Write(payload)          // 首包数据
        }
        _, err = c.Conn.Write(buf.Bytes())
    })
    return written, err
}
```

### UDP 数据包格式

UDP 通过 TCP 连接承载（UDP over TCP），使用 `Associate` 命令建立连接后，每个 UDP 数据包的格式为：

```
+---------------------+
| ATYP (1 byte)       |
+---------------------+
| DST.ADDR (variable) |
+---------------------+
| DST.PORT (2 bytes)  |
+---------------------+
| Length (2 bytes)    |   UDP 载荷长度，大端序
+---------------------+
| CRLF                |   0x0D 0x0A
+---------------------+
| Payload (Length B)  |
+---------------------+
```

源码参考 `tunnel/trojan/packet.go` 中 `PacketConn.WriteWithMetadata`。

## CMD 类型

| 值 | 名称 | 说明 |
|----|------|------|
| `0x01` | Connect | TCP 连接代理，最常用 |
| `0x03` | Associate | UDP 关联，用于 UDP 代理 |
| `0x7F` | Mux | 多路复用连接（Trojan-Go-Next 扩展） |

源码定义于 `tunnel/trojan/client.go`：

```go
const (
    Connect   tunnel.Command = 1
    Associate tunnel.Command = 3
    Mux       tunnel.Command = 0x7f
)
```

## ATYP 类型

| 值 | 名称 | 地址长度 | 说明 |
|----|------|----------|------|
| `0x01` | IPv4 | 4 字节 + 2 字节端口 | IPv4 地址 |
| `0x03` | Domain | 1 字节长度 + 域名 + 2 字节端口 | 域名地址 |
| `0x04` | IPv6 | 16 字节 + 2 字节端口 | IPv6 地址 |

源码定义于 `tunnel/metadata.go`：

```go
const (
    IPv4       AddressType = 1
    DomainName AddressType = 3
    IPv6       AddressType = 4
)
```

### 地址编码示例

**IPv4**（`example: 1.2.3.4:443`）：
```
01              ATYP = IPv4
01 02 03 04     IP 地址（4 字节）
01 BB           端口（443 = 0x01BB，大端序）
```

**域名**（`example: example.com:443`）：
```
03              ATYP = Domain
0B              域名长度（11 字节）
65 78 61 6D ... 域名（"example.com"）
01 BB           端口（443 = 0x01BB，大端序）
```

**IPv6**（`example: [::1]:443`）：
```
04              ATYP = IPv6
00 00 00 00     IP 地址
00 00 00 00     （共 16 字节）
00 00 00 01
01 BB           端口（443 = 0x01BB，大端序）
```

## 密码哈希

Trojan 使用 SHA-224 对用户密码进行哈希，输出为 56 字符的十六进制小写字符串。

```go
// common/common.go
func SHA224String(password string) string {
    hash := sha256.New224()
    hash.Write([]byte(password))
    val := hash.Sum(nil)
    str := ""
    for _, v := range val {
        str += fmt.Sprintf("%02x", v)
    }
    return str  // 56 字符，如 "a3b2c1d4e5f6..."
}
```

客户端在配置中填写明文密码，运行时自动计算 SHA-224 哈希。服务端存储和比较的是哈希值。

## 服务端行为

服务端收到新连接后的处理流程：

```
客户端连接
    │
    ▼
读取 56 字节密码哈希
    │
    ▼
读取 CRLF
    │
    ▼
验证密码哈希
    │
    ├── 哈希有效 ────────────────────┐
    │                                │
    ├── 哈希无效 / 读取失败          │
    │       │                        │
    │       ▼                        │
    │   Rewind 连接缓冲区            │
    │       │                        │
    │       ▼                        │
    │   重定向到 fallback 服务器      │
    │   （伪装为真实 HTTP 服务）      │
    │                                │
    ▼                                ▼
读取 CMD + 目标地址              正常代理连接
    │
    ├── Connect → 转发 TCP
    ├── Associate → 转发 UDP
    └── Mux → 多路复用通道
```

### 关键实现细节

1. **Rewind 机制**：服务端使用 `common.NewRewindConn` 包装连接，设置 128 字节缓冲区。当密码校验失败时，调用 `Rewind()` 将已读数据放回缓冲区，然后将整个连接（包括客户端发送的所有探测数据）转发给真实的 fallback 服务器。

2. **Fallback 地址**：配置中的 `remote_host` + `remote_port` 指定 fallback 服务器，通常是本机运行的 HTTP/HTTPS 服务器（如 Nginx、Caddy）。

3. **首包超时刷新**：客户端在发送请求头后，如果 100ms 内没有更多数据要发送，会自动刷新空的 payload，确保服务端能立即解析请求头而不必等待后续数据。

源码参考 `tunnel/trojan/server.go` 中 `Server.acceptLoop` 和 `InboundConn.Auth`。

## 连接流程示意

一个完整的 TCP 代理连接流程：

```
客户端                                服务端
  │                                     │
  │──── TLS 握手 ──────────────────────▶│
  │                                     │
  │──── Hash + CRLF + CMD + ADDR ──────▶│
  │      + CRLF + (Payload)             │
  │                                     │── 验证密码
  │                                     │── 解析目标地址
  │                                     │── 建立到目标的连接
  │                                     │
  │◀──── TLS 加密的双向数据转发 ────────▶│
  │                                     │
```

一个认证失败的连接（主动探测）：

```
探测方                                  服务端
  │                                     │
  │──── TLS 握手 ──────────────────────▶│
  │                                     │
  │──── 任意数据 ──────────────────────▶│
  │                                     │── 密码校验失败
  │                                     │── Rewind 缓冲区
  │                                     │── 连接到 fallback 服务器
  │                                     │
  │◀──── 与真实 HTTP 服务器交互 ────────▶│
  │      （看起来是正常的网站）            │
```
