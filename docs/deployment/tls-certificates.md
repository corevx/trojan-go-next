---
title: TLS 证书管理
---

# TLS 证书管理

Trojan-Go-Next 依赖 TLS 加密传输。本文介绍证书的申请、配置和续期。

## 证书类型对比

| 类型 | 费用 | 安全性 | 推荐场景 |
|------|------|--------|----------|
| Let's Encrypt | 免费 | 高 | 生产环境首选 |
| 商业 CA 证书 | 付费 | 高 | 企业用户 |
| 自签证书 | 免费 | 低 | 测试/开发 |

## 使用 Let's Encrypt（推荐）

### 申请证书

```shell
# 安装 certbot
sudo apt install certbot    # Debian/Ubuntu
sudo yum install certbot    # CentOS

# 申请证书（standalone 模式，需要 80 端口空闲）
sudo certbot certonly --standalone -d example.com
```

::: tip
申请前确保：
1. 域名 A 记录已指向服务器 IP
2. 服务器 80 端口未被占用（或停止占用 80 端口的服务）
:::

### 证书文件位置

```
/etc/letsencrypt/live/example.com/
├── fullchain.pem    → 配置中的 cert
├── privkey.pem      → 配置中的 key
└── chain.pem        → 证书链（一般不需要直接使用）
```

### 配置 Trojan-Go-Next

```json
{
    "ssl": {
        "cert": "/etc/letsencrypt/live/example.com/fullchain.pem",
        "key": "/etc/letsencrypt/live/example.com/privkey.pem"
    }
}
```

### 自动续期

Let's Encrypt 证书有效期 90 天。certbot 安装时通常会自动配置续期定时任务。

```shell
# 检查自动续期是否已配置
systemctl list-timers | grep certbot

# 测试续期流程
sudo certbot renew --dry-run
```

配置续期后自动重启 Trojan-Go-Next，创建 `/etc/letsencrypt/renewal-hooks/post/restart-trojan-go-next.sh`：

```shell
#!/bin/bash
systemctl restart trojan-go-next
```

```shell
sudo chmod +x /etc/letsencrypt/renewal-hooks/post/restart-trojan-go-next.sh
```

## 使用 DNS 验证申请证书

如果你的服务器 80 端口被 Trojan-Go-Next 或其他服务占用，可以使用 DNS 验证：

```shell
# 以 Cloudflare DNS 插件为例
sudo apt install python3-certbot-dns-cloudflare
sudo certbot certonly \
    --dns-cloudflare \
    --dns-cloudflare-credentials /etc/letsencrypt/cloudflare.ini \
    -d example.com
```

## 使用自签证书（仅用于测试）

::: warning
自签证书不推荐用于生产环境。GFW 可能通过证书特征识别代理节点。
:::

```shell
openssl req -x509 -newkey rsa:4096 \
    -keyout key.pem -out cert.crt \
    -days 365 -nodes \
    -subj "/CN=example.com"
```

客户端需要配置跳过证书验证：

```json
{
    "ssl": {
        "verify": false,
        "sni": "example.com"
    }
}
```

## 证书热重载

Trojan-Go-Next 支持证书热重载，无需重启服务：

```json
{
    "ssl": {
        "cert": "/etc/letsencrypt/live/example.com/fullchain.pem",
        "key": "/etc/letsencrypt/live/example.com/privkey.pem",
        "cert_check_rate": 3600
    }
}
```

`cert_check_rate` 设置证书文件检查间隔（秒），检测到文件变化后自动重载。

## 证书到期告警 (v0.11.0)

Trojan-Go-Next v0.11.0 新增证书有效期检测：

- 自动检测证书到期时间
- 证书即将过期时在日志中输出告警
- 配合监控服务可在 Prometheus 中看到证书状态

## 常见问题

### 证书验证失败

```
certificate verify failed
```

排查：
1. 检查证书是否过期：`openssl x509 -in cert.pem -noout -dates`
2. 确认域名和证书匹配：`openssl x509 -in cert.pem -noout -text | grep -A1 "Subject Alternative"`
3. 确认 SNI 配置正确

### 证书申请失败

```
Failed to connect to example.com:443
```

排查：
1. 确认域名 A 记录正确：`nslookup example.com`
2. 确认 80 端口可从外网访问
3. 临时停止占用 80 端口的服务
