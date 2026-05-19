---
title: 安装指南
---

# 安装指南

本文将引导你从零开始部署 Trojan-Go-Next。无论你是第一次接触代理工具，还是从其他工具迁移过来，都可以按以下步骤快速上手。

## 准备工作

在开始之前，你需要准备：

| 项目 | 说明 |
|------|------|
| 一台服务器 | 位于防火墙之外（境外）的 VPS，推荐使用 Ubuntu / Debian / CentOS |
| 一个域名 | 可以使用免费域名（如 .tk），也可以使用付费域名 |
| TLS 证书 | 可通过 Let's Encrypt 免费申请，Trojan-Go-Next 也支持自签证书 |
| 域名解析 | 将域名 A 记录指向你的服务器 IP |

::: tip
Trojan-Go-Next 兼容原版 Trojan 协议。如果你之前使用过原版 Trojan，可以直接迁移配置文件，无需修改。
:::

## 下载安装

### 方法一：下载预编译二进制（推荐）

从 [Release 页面](https://github.com/corevx/trojan-go-next-next/releases) 下载对应平台的压缩包，解压后即可使用。

```shell
# 以 Linux amd64 为例
wget https://github.com/corevx/trojan-go-next-next/releases/latest/download/trojan-go-next-linux-amd64.zip
unzip trojan-go-next-linux-amd64.zip
chmod +x trojan-go-next
```

支持的架构：

| 平台 | 架构 |
|------|------|
| Linux | amd64, 386, arm, arm64, mips, mipsle, mips64, mips64le |
| macOS | amd64 (Intel), arm64 (Apple Silicon) |
| Windows | amd64, 386, arm, arm64 |
| FreeBSD | amd64, 386 |

### 方法二：Docker 部署

```shell
docker pull ghcr.io/corevx/trojan-go-next-next
```

运行：

```shell
docker run --name trojan-go-next -d \
    -v /etc/trojan-go-next/:/etc/trojan-go-next \
    --network host \
    ghcr.io/corevx/trojan-go-next-next
```

指定配置文件路径：

```shell
docker run --name trojan-go-next -d \
    -v /path/to/host/config:/path/in/container \
    --network host \
    ghcr.io/corevx/trojan-go-next-next \
    /path/in/container/config.json
```

### 方法三：从源码编译

需要 Go >= 1.22：

```shell
git clone https://github.com/corevx/trojan-go-next-next.git
cd trojan-go-next
make
```

编译产物在 `build/` 目录中。

## 申请 TLS 证书

Trojan-Go-Next 依赖 TLS 加密，你需要一个有效的 TLS 证书。推荐使用 Let's Encrypt 免费证书：

```shell
# 安装 certbot
sudo apt install certbot   # Debian/Ubuntu
sudo yum install certbot   # CentOS

# 申请证书（将 example.com 替换为你的域名）
sudo certbot certonly --standalone -d example.com
```

证书文件位于 `/etc/letsencrypt/live/example.com/` 目录：
- `fullchain.pem` — 证书链（对应配置中的 `cert`）
- `privkey.pem` — 私钥（对应配置中的 `key`）

::: warning
确保域名的 A 记录已正确指向服务器 IP，否则证书申请会失败。
:::

## 最简部署（简易模式）

如果你只想快速测试，可以使用简易模式启动服务端：

```shell
sudo ./trojan-go-next -server \
    -remote 127.0.0.1:80 \
    -local 0.0.0.0:443 \
    -key /etc/letsencrypt/live/example.com/privkey.pem \
    -cert /etc/letsencrypt/live/example.com/fullchain.pem \
    -password your_password
```

客户端：

```shell
./trojan-go-next -client \
    -remote example.com:443 \
    -local 127.0.0.1:1080 \
    -password your_password
```

启动后，本地 `127.0.0.1:1080` 就是一个 SOCKS5 / HTTP 代理端口了。

## 配置文件部署（推荐）

简易模式适合快速测试。正式使用建议创建配置文件。

### 服务端配置

创建 `/etc/trojan-go-next/server.json`：

```json
{
    "run_type": "server",
    "local_addr": "0.0.0.0",
    "local_port": 443,
    "remote_addr": "127.0.0.1",
    "remote_port": 80,
    "password": [
        "your_awesome_password"
    ],
    "ssl": {
        "cert": "/etc/letsencrypt/live/example.com/fullchain.pem",
        "key": "/etc/letsencrypt/live/example.com/privkey.pem"
    }
}
```

::: tip
`remote_addr:remote_port` 指向一个本地 HTTP 服务。当非 Trojan 流量（如浏览器访问、GFW 主动探测）到达时，Trojan-Go-Next 会将请求转发到这个 HTTP 服务，使其看起来像一个正常的 HTTPS 网站。
:::

启动服务端：

```shell
./trojan-go-next -config /etc/trojan-go-next/server.json
```

### 客户端配置

创建 `client.json`：

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "example.com",
    "remote_port": 443,
    "password": [
        "your_awesome_password"
    ]
}
```

启动客户端：

```shell
./trojan-go-next -config client.json
```

## 配置为系统服务

如果你使用 Linux 服务器，推荐将 Trojan-Go-Next 配置为 systemd 服务，实现开机自启和自动重启。

### 方法一：使用 make install

```shell
# 编译并安装
make
sudo make install
```

这会自动：
- 将二进制文件安装到 `/usr/bin/trojan-go-next`
- 将示例配置复制到 `/etc/trojan-go-next/`
- 安装 systemd 服务文件
- 下载 GeoIP / GeoSite 数据文件

### 方法二：手动配置

1. 复制二进制文件：

```shell
sudo cp trojan-go-next /usr/bin/trojan-go-next
```

2. 创建 systemd 服务文件 `/usr/lib/systemd/system/trojan-go-next.service`：

```ini
[Unit]
Description=Trojan-Go-Next
After=network.target nss-lookup.target

[Service]
User=nobody
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=/usr/bin/trojan-go-next -config /etc/trojan-go-next/config.json
Restart=on-failure
RestartSec=10s
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
```

3. 启用并启动服务：

```shell
sudo systemctl daemon-reload
sudo systemctl enable trojan-go-next
sudo systemctl start trojan-go-next
```

4. 查看运行状态：

```shell
sudo systemctl status trojan-go-next
```

5. 查看日志：

```shell
journalctl -u trojan-go-next -f
```

## 验证部署

### 验证服务端

1. 使用浏览器访问 `https://example.com`，应该能看到你服务器上的正常网页
2. 检查 Trojan-Go-Next 进程是否正常运行：

```shell
ps aux | grep trojan-go-next
```

3. 检查端口是否在监听：

```shell
ss -tlnp | grep 443
```

### 验证客户端

1. 启动客户端后，使用 curl 测试代理是否工作：

```shell
# 通过 SOCKS5 代理访问
curl --socks5 127.0.0.1:1080 https://www.google.com -I

# 或通过 HTTP 代理访问
curl --proxy http://127.0.0.1:1080 https://www.google.com -I
```

如果返回 `HTTP 200`，说明代理工作正常。

2. 配置浏览器使用代理：
   - SOCKS5 代理：`127.0.0.1:1080`
   - HTTP 代理：`127.0.0.1:1080`

::: tip
Trojan-Go-Next 的代理端口同时支持 SOCKS5 和 HTTP 协议，无需额外配置。
:::

## 下一步

- [配置入门](/guide/config) — 了解完整的配置选项
- [Trojan 原理入门](/guide/trojan) — 理解 Trojan 协议如何绕过 GFW
- [常见问题](/guide/faq) — 遇到问题？先看看这里
