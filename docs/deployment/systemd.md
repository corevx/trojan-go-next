---
title: systemd 服务部署
---

# systemd 服务部署

将 Trojan-Go 配置为 systemd 服务，实现开机自启和故障自动重启。

## 方法一：make install（推荐）

如果你从源码编译：

```shell
make
sudo make install
```

这会自动完成以下操作：

| 操作 | 目标路径 |
|------|----------|
| 安装二进制 | `/usr/bin/trojan-go` |
| 复制示例配置 | `/etc/trojan-go/` |
| 安装 systemd 服务 | `/usr/lib/systemd/system/` |
| 下载 GeoIP/GeoSite 数据 | `/usr/share/trojan-go/` |

## 方法二：手动配置

### 1. 安装二进制

```shell
sudo cp trojan-go /usr/bin/trojan-go
```

### 2. 创建配置文件

将你的配置文件放到 `/etc/trojan-go/config.json`。

### 3. 创建服务文件

创建 `/usr/lib/systemd/system/trojan-go.service`：

```ini
[Unit]
Description=Trojan-Go
Documentation=https://corevx.github.io/trojan-go-next/
After=network.target nss-lookup.target

[Service]
User=nobody
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=/usr/bin/trojan-go -config /etc/trojan-go/config.json
Restart=on-failure
RestartSec=10s
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
```

### 4. 启动服务

```shell
sudo systemctl daemon-reload
sudo systemctl enable trojan-go    # 开机自启
sudo systemctl start trojan-go     # 立即启动
```

## 常用操作

```shell
# 查看运行状态
sudo systemctl status trojan-go

# 查看实时日志
journalctl -u trojan-go -f

# 重启服务（修改配置后）
sudo systemctl restart trojan-go

# 停止服务
sudo systemctl stop trojan-go

# 取消开机自启
sudo systemctl disable trojan-go
```

## 多实例部署

如果你需要同时运行多个 Trojan-Go 实例（不同配置），可以使用模板服务文件。

创建 `/usr/lib/systemd/system/trojan-go@.service`：

```ini
[Unit]
Description=Trojan-Go (%i)
After=network.target nss-lookup.target

[Service]
User=nobody
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=/usr/bin/trojan-go -config /etc/trojan-go/%i.json
Restart=on-failure
RestartSec=10s
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
```

使用方法：

```shell
# 将配置文件放到 /etc/trojan-go/ 下
# 例如 /etc/trojan-go/ws.json、/etc/trojan-go/direct.json

sudo systemctl enable trojan-go@ws
sudo systemctl start trojan-go@ws

sudo systemctl enable trojan-go@direct
sudo systemctl start trojan-go@direct
```

## 安全加固说明

服务文件中的安全相关字段：

| 字段 | 说明 |
|------|------|
| `User=nobody` | 以最低权限用户运行 |
| `CapabilityBoundingSet` | 仅授予网络管理和服务端口绑定权限 |
| `AmbientCapabilities` | 允许上述权限在非 root 用户下生效 |
| `NoNewPrivileges=true` | 禁止提权 |
| `LimitNOFILE=infinity` | 不限制文件描述符数量（支持高并发） |

## 证书自动续期

使用 Let's Encrypt 证书时，配置自动续期后重启服务：

```shell
# 测试续期
sudo certbot renew --dry-run

# 确认 cron 定时任务已安装
systemctl list-timers | grep certbot

# 如果需要手动添加续期后的重启钩子
# 编辑 /etc/letsencrypt/renewal-hooks/post/restart-trojan-go.sh
```

创建 `/etc/letsencrypt/renewal-hooks/post/restart-trojan-go.sh`：

```shell
#!/bin/bash
systemctl restart trojan-go
```

```shell
sudo chmod +x /etc/letsencrypt/renewal-hooks/post/restart-trojan-go.sh
```
