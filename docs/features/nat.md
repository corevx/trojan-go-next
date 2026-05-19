---
title: "透明代理"
---

# 透明代理

::: warning
Trojan 原版不完全支持此特性（UDP 部分有差异）。此功能为 Trojan-Go 扩展。
:::

## 功能概述

NAT 模式基于 Linux 内核的 TProxy（Transparent Proxy）机制实现透明代理。与客户端模式不同，透明代理无需终端设备配置代理设置——所有经过网关的 TCP/UDP 流量会被自动劫持，通过 Trojan TLS 隧道发送到远端服务器。

透明代理的核心价值在于：网络中的所有设备（手机、智能电视、IoT 设备等）无需任何配置即可自动走代理通道。

```
+--------+     +------------------+     Trojan TLS     +------------+
| 局域网  | --> | Linux 网关/路由器 | ──────────────> | 远端服务器  | --> 互联网
| 设备    |     | (TProxy + NAT)   |     (加密传输)     |            |
+--------+     +------------------+                    +------------+
```

## 适用场景

- 家庭路由器或软路由场景，希望全网设备透明代理
- 企业网关部署，为内网所有终端提供统一代理出口
- 旁路由/网桥模式，仅劫持特定设备的流量

::: warning
此功能仅支持 Linux 系统，需要内核支持 TProxy 模块。
:::

## 配置方法

### Trojan-Go 配置

将一份正确的客户端配置中的 `run_type` 修改为 `"nat"`，并按需调整 `local_port`：

```json
{
    "run_type": "nat",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "your-server-domain.com",
    "remote_port": 443,
    "password": [
        "your-strong-password"
    ]
}
```

`local_port` 是 TProxy 监听端口，需要与 iptables 规则中的 `--on-port` 一致。

### iptables 规则（TCP + UDP）

假设网关有两个网卡：`$INTERFACE`（局域网网卡）和另一块连接互联网的网卡。以下规则将局域网网卡进入的流量转交给 Trojan-Go：

```bash
#!/bin/bash

# 请替换以下变量
SERVER_IP="your-server-ip"
TROJAN_GO_PORT=1080
INTERFACE="eth0"

# 新建 TROJAN_GO 链
iptables -t mangle -N TROJAN_GO

# 绕过 Trojan-Go 服务器地址（避免回环）
iptables -t mangle -A TROJAN_GO -d $SERVER_IP -j RETURN

# 绕过保留/私有地址
iptables -t mangle -A TROJAN_GO -d 0.0.0.0/8 -j RETURN
iptables -t mangle -A TROJAN_GO -d 10.0.0.0/8 -j RETURN
iptables -t mangle -A TROJAN_GO -d 127.0.0.0/8 -j RETURN
iptables -t mangle -A TROJAN_GO -d 169.254.0.0/16 -j RETURN
iptables -t mangle -A TROJAN_GO -d 172.16.0.0/12 -j RETURN
iptables -t mangle -A TROJAN_GO -d 192.168.0.0/16 -j RETURN
iptables -t mangle -A TROJAN_GO -d 224.0.0.0/4 -j RETURN
iptables -t mangle -A TROJAN_GO -d 240.0.0.0/4 -j RETURN

# 未命中上述规则的流量，打上标记并转交 TProxy
iptables -t mangle -A TROJAN_GO -p tcp -j TPROXY --on-port $TROJAN_GO_PORT --tproxy-mark 0x01/0x01
iptables -t mangle -A TROJAN_GO -p udp -j TPROXY --on-port $TROJAN_GO_PORT --tproxy-mark 0x01/0x01

# 从局域网网卡进入的 TCP/UDP 流量，跳转到 TROJAN_GO 链
iptables -t mangle -A PREROUTING -p tcp -i $INTERFACE -j TROJAN_GO
iptables -t mangle -A PREROUTING -p udp -i $INTERFACE -j TROJAN_GO

# 路由规则：打上标记的包重新进入本地回环
ip route add local default dev lo table 100
ip rule add fwmark 1 lookup 100
```

### ip6tables 规则（IPv6）

如果网络环境使用 IPv6，需要额外配置 ip6tables：

```bash
# 新建 TROJAN_GO_V6 链
ip6tables -t mangle -N TROJAN_GO_V6

# 绕过服务器 IPv6 地址
ip6tables -t mangle -A TROJAN_GO_V6 -d $SERVER_IPV6 -j RETURN

# 绕过本地/链路本地地址
ip6tables -t mangle -A TROJAN_GO_V6 -d ::1/128 -j RETURN
ip6tables -t mangle -A TROJAN_GO_V6 -d fe80::/10 -j RETURN
ip6tables -t mangle -A TROJAN_GO_V6 -d ff00::/8 -j RETURN
ip6tables -t mangle -A TROJAN_GO_V6 -d fc00::/7 -j RETURN

# 转发至 TProxy
ip6tables -t mangle -A TROJAN_GO_V6 -p tcp -j TPROXY --on-port $TROJAN_GO_PORT --tproxy-mark 0x01/0x01
ip6tables -t mangle -A TROJAN_GO_V6 -p udp -j TPROXY --on-port $TROJAN_GO_PORT --tproxy-mark 0x01/0x01

# 绑定局域网网卡
ip6tables -t mangle -A PREROUTING -p tcp -i $INTERFACE -j TROJAN_GO_V6
ip6tables -t mangle -A PREROUTING -p udp -i $INTERFACE -j TROJAN_GO_V6

# IPv6 路由规则
ip -6 route add local default dev lo table 100
ip -6 rule add fwmark 1 lookup 100
```

### nftables 替代方案

如果你的系统使用 nftables 而非 iptables，可以使用以下配置：

```bash
#!/bin/bash

SERVER_IP="your-server-ip"
TROJAN_GO_PORT=1080
INTERFACE="eth0"

# 创建表和链
nft add table mangle trojan_go
nft add chain mangle trojan_go prerouting '{ type filter hook prerouting priority mangle; }'

# 绕过服务器地址和私有地址
nft add rule mangle trojan_go prerouting ip daddr $SERVER_IP return
nft add rule mangle trojan_go prerouting ip daddr { 0.0.0.0/8, 10.0.0.0/8, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 240.0.0.0/4 } return

# TProxy 转发
nft add rule mangle trojan_go prerouting iifname $INTERFACE tcp dport != 0 tproxy to :$TROJAN_GO_PORT meta mark set 0x1
nft add rule mangle trojan_go prerouting iifname $INTERFACE udp dport != 0 tproxy to :$TROJAN_GO_PORT meta mark set 0x1

# 路由规则
ip route add local default dev lo table 100
ip rule add fwmark 1 lookup 100
```

### DNS 处理

::: warning
透明代理本身不处理 DNS 请求的劫持。你需要额外配置 DNS 转发或使用 dnsmasq 等工具，确保局域网设备的 DNS 查询也通过代理通道发送，避免 DNS 泄漏。
:::

推荐方案：

1. 在 iptables 中增加 DNS 劫持规则，将发往 `53` 端口的 UDP 流量也转发给 Trojan-Go
2. 配合 FORWARD 模式，在网关本地搭建无污染 DNS（参考 [隧道与反向代理](./forward)）
3. 在网关上运行 dnsmasq，将 DNS 上游指向本地 FORWARD 端口

## 启动

配置完成后，**以 root 权限启动** Trojan-Go：

```bash
sudo trojan-go -config config.json
```

TProxy 需要 root 权限才能监听和操作网络数据包。

## 故障排查

### TPROXY 模块未加载

**现象**：iptables 规则添加失败，提示 `No such file or directory` 或 `can't initialize iptables table mangle`。

**解决方法**：

```bash
# 加载 TProxy 内核模块
sudo modprobe xt_TPROXY

# 确认模块已加载
lsmod | grep TPROXY

# 如需开机自动加载
echo "xt_TPROXY" | sudo tee /etc/modules-load.d/tproxy.conf
```

### 权限不足

**现象**：Trojan-Go 启动后无法绑定 TProxy 端口。

**解决方法**：确保使用 root 权限启动：

```bash
sudo trojan-go -config config.json
```

或为二进制文件授予网络能力（不推荐，存在安全风险）：

```bash
sudo setcap cap_net_admin,cap_net_bind_service+ep /usr/bin/trojan-go
```

### 连接回环

**现象**：流量不断循环，网关 CPU 飙高。

**解决方法**：确认 iptables 规则中已正确添加服务器 IP 的绕过规则（`-d $SERVER_IP -j RETURN`）。如果缺少此规则，发往 Trojan 服务器的流量会被再次劫持，形成回环。

### 验证透明代理

```bash
# 1. 确认 iptables 规则已生效
sudo iptables -t mangle -L TROJAN_GO -v -n

# 2. 确认路由规则已生效
ip rule list
ip route list table 100

# 3. 从局域网设备测试连通性
curl -v https://www.google.com

# 4. 检查 Trojan-Go 日志中是否有连接记录
# 日志级别设为 0 (Debug) 或 1 (Info) 可以看到详细连接信息
```
