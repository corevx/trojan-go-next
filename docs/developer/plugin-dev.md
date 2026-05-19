---
title: "传输层插件开发"
---

# 传输层插件开发

Trojan-Go 鼓励开发传输层插件，以丰富协议类型，增加对抗主动探测的战略纵深。传输层插件替代 TLS 层进行传输加密和混淆，与 Trojan-Go 本体通过 TCP Socket 通信，完全解耦。

## 设计原则

Trojan-Go 的插件设计原则与 Shadowsocks SIP003 有所不同，遵循以下三条：

### 1. 加密、验证与抗重放

插件必须对传输内容进行加密、混淆和完整性校验，并能抵抗重放攻击。

这是因为 Trojan 协议本身不加密。将 TLS 替换为传输层插件后，Trojan-Go 将 **完全信任插件的安全性**。TLS 提供的所有安全属性（机密性、完整性、身份认证、前向安全）需要由插件保证。

### 2. 伪装真实服务

插件应伪装成一种已有的、常见的服务（记做 X 服务）及其流量，在此基础上嵌入自己的加密内容。

最适合隐藏一棵树的地方是森林。如果一个插件伪装成 MySQL 服务，那么它的流量在外观上应与真实 MySQL 协议一致。

### 3. 失败时 Fallback

服务端的插件在检测到内容被篡改或遭到重放时，**必须将此连接交由 Trojan-Go 处理**，而不是直接断开。具体步骤：

1. 将已读入和未读入的内容一并发送给 Trojan-Go
2. Trojan-Go 将连接重定向到一个真实的 X 服务器
3. 攻击者开始与真实的 X 服务器交互

这充分利用了 Trojan-Go 的抗主动探测特性。即使 GFW 对服务器进行主动探测，服务器也能表现得与真实 X 服务一致。

### 示例场景

```
1. 防火墙发现你的 "MySQL" 服务器流量异常，发起主动探测
2. 探测连接到达插件，插件校验发现这不是合法代理流量
3. 插件将连接（含探测载荷）交给 Trojan-Go
4. Trojan-Go 将连接重定向到真实 MySQL 服务器
5. 防火墙与真实 MySQL 服务器交互，无法发现异常，无法封禁
```

::: tip
即使你的插件不能完全满足原则 2 和 3，甚至加密强度有限，我们同样鼓励开发。GFW 主要针对流行协议进行审计和封锁，自研协议只要不公开发布，同样能保持强健的生命力。
:::

## Transport 隧道接口

插件在隧道栈中替代 TLS 层的位置：

```
正常协议栈：     trojan → tls → transport → TCP
使用插件后：     trojan → plugin → transport → TCP
```

Trojan-Go 内部的隧道接口定义于 `tunnel/tunnel.go`，插件无需实现这些接口，而是通过 TCP Socket 与 Trojan-Go 通信：

```go
// Tunnel 描述一个隧道层
type Tunnel interface {
    Name() string
    NewClient(context.Context, Client) (Client, error)
    NewServer(context.Context, Server) (Server, error)
}

// Client 出站连接
type Client interface {
    Dialer   // DialConn + DialPacket
    io.Closer
}

// Server 入站监听
type Server interface {
    Listener // AcceptConn + AcceptPacket
    io.Closer
}
```

插件通过配置项 `plugin` 和 `plugin_opts` 激活，Trojan-Go 会启动插件进程并将其标准输入/输出与 transport 层的 TCP 连接对接。

## SIP003 兼容性

Trojan-Go 插件建议参照 [SIP003](https://shadowsocks.org/en/spec/Plugin.html) 标准开发。符合 SIP003 的插件可同时用于 Trojan-Go 和 Shadowsocks。

### 环境变量

插件启动时，Trojan-Go 通过环境变量传递连接信息：

| 环境变量 | 说明 |
|----------|------|
| `SS_LOCAL_HOST` | 本地监听地址（插件需要监听的地址） |
| `SS_LOCAL_PORT` | 本地监听端口 |
| `SS_REMOTE_HOST` | 远端地址（Trojan-Go 服务端地址） |
| `SS_REMOTE_PORT` | 远端端口 |
| `SS_PLUGIN_OPTIONS` | 插件选项（对应配置中的 `plugin_opts`） |

### 通信模型

```
客户端侧：

  Trojan-Go → TCP → Plugin Client → 自定义协议 → 网络

  SS_LOCAL_HOST:PORT = Trojan-Go 连接的目标
  SS_REMOTE_HOST:PORT = Plugin 发出的目标


服务端侧：

  网络 → 自定义协议 → Plugin Server → TCP → Trojan-Go

  SS_LOCAL_HOST:PORT = Plugin 监听地址
  SS_REMOTE_HOST:PORT = Trojan-Go 监听地址
```

## 插件生命周期

```
┌─────────────────────────────────────────────┐
│                插件生命周期                    │
│                                             │
│  1. Start（启动）                             │
│     ├── 读取环境变量                          │
│     ├── 解析插件选项                          │
│     └── 初始化加密/混淆/连接                   │
│                                             │
│  2. Handshake（握手）                         │
│     ├── 客户端：连接远端并完成协议握手          │
│     └── 服务端：接受连接并验证                  │
│                                             │
│  3. Relay（中继）                             │
│     ├── 双向数据转发                          │
│     ├── 加密/解密                             │
│     └── 混淆/反混淆                           │
│                                             │
│  4. Shutdown（关闭）                          │
│     ├── 连接断开或错误                        │
│     └── 清理资源                              │
└─────────────────────────────────────────────┘
```

### 启动阶段

Trojan-Go 通过配置启动插件进程：

```json
{
  "transport": {
    "plugin": "my-plugin",
    "plugin_opts": "server;key=value"
  }
}
```

插件进程启动后，通过 `SS_LOCAL_HOST:SS_LOCAL_PORT` 与 Trojan-Go 通信，通过 `SS_REMOTE_HOST:SS_REMOTE_PORT` 与远端通信。

### 握手与中继

插件在 TCP 连接建立后执行协议握手，完成后进入双向中继模式：

```
Trojan-Go ←→ SS_LOCAL ←→ Plugin ←→ SS_REMOTE ←→ 网络
                 TCP          自定义协议           TCP
```

## 代码示例骨架

以下是一个 Go 语言实现的插件骨架，遵循 SIP003 标准：

```go
package main

import (
    "io"
    "log"
    "net"
    "os"
    "strconv"
    "sync"
)

func main() {
    // 1. Start: 读取环境变量
    localHost := os.Getenv("SS_LOCAL_HOST")
    localPort := os.Getenv("SS_LOCAL_PORT")
    remoteHost := os.Getenv("SS_REMOTE_HOST")
    remotePort := os.Getenv("SS_REMOTE_PORT")
    opts := os.Getenv("SS_PLUGIN_OPTIONS")

    localAddr := net.JoinHostPort(localHost, localPort)
    remoteAddr := net.JoinHostPort(remoteHost, remotePort)

    log.Printf("plugin started: local=%s remote=%s opts=%s",
        localAddr, remoteAddr, opts)

    // 2. 监听本地端口
    ln, err := net.Listen("tcp", localAddr)
    if err != nil {
        log.Fatal(err)
    }
    defer ln.Close()

    for {
        conn, err := ln.Accept()
        if err != nil {
            log.Fatal(err)
        }
        go handleConn(conn, remoteAddr)
    }
}

func handleConn(localConn net.Conn, remoteAddr string) {
    defer localConn.Close()

    // 3. Handshake: 建立到远端的连接
    remoteConn, err := net.Dial("tcp", remoteAddr)
    if err != nil {
        log.Printf("dial remote failed: %v", err)
        return
    }
    defer remoteConn.Close()

    // TODO: 在此处实现你的协议握手
    // - 身份验证
    // - 密钥协商
    // - 混淆协商

    // 4. Relay: 双向数据转发
    var wg sync.WaitGroup
    wg.Add(2)

    // local → remote（加密方向）
    go func() {
        defer wg.Done()
        // TODO: 加密/混淆数据后写入 remoteConn
        io.Copy(remoteConn, localConn)
    }()

    // remote → local（解密方向）
    go func() {
        defer wg.Done()
        // TODO: 解密/反混淆数据后写入 localConn
        io.Copy(localConn, remoteConn)
    }()

    wg.Wait()
}
```

### 编译插件

```bash
# 编译为独立可执行文件
go build -o my-plugin .

# 放置到 PATH 可找到的位置
sudo cp my-plugin /usr/local/bin/
```

### Trojan-Go 配置使用

```json
{
  "run_type": "client",
  "transport": {
    "plugin": "my-plugin",
    "plugin_opts": "key1=value1;key2=value2"
  }
}
```

## 测试插件

### 基本连通性测试

1. 启动 Trojan-Go 服务端（不使用插件）
2. 启动插件服务端模式，指向 Trojan-Go 监听地址
3. 启动插件客户端模式
4. 配置 Trojan-Go 客户端连接到插件客户端地址

```bash
# 终端 1: Trojan-Go 服务端
trojan-go -config server.json  # 监听 127.0.0.1:8443

# 终端 2: 插件服务端
SS_LOCAL_HOST=127.0.0.1 SS_LOCAL_PORT=443 \
SS_REMOTE_HOST=127.0.0.1 SS_REMOTE_PORT=8443 \
my-plugin

# 终端 3: 插件客户端
SS_LOCAL_HOST=127.0.0.1 SS_LOCAL_PORT=1080 \
SS_REMOTE_HOST=your-server.com SS_REMOTE_PORT=443 \
my-plugin

# 终端 4: Trojan-Go 客户端连接到 127.0.0.1:1080
```

### 主动探测测试

验证 fallback 行为是否符合预期：

```bash
# 使用 OpenSSL 发送非插件协议数据
echo -e "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n" | \
  openssl s_client -connect your-server:443 -quiet

# 应看到真实 fallback 服务器（如 Nginx）的响应
```

### 性能测试

```bash
# 使用 iperf3 测试吞吐量
iperf3 -c 127.0.0.1 -p 1080 --connect-timeout 3000

# 检查延迟
curl -x socks5://127.0.0.1:1080 -o /dev/null -w "TCP: %{time_connect}s\nTotal: %{time_total}s\n" \
  https://www.google.com
```
