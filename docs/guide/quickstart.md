---
title: 5 分钟快速入门
---

# 5 分钟快速入门

本页面向第一次使用 Trojan-Go-Next 的用户。如果你已经熟悉 Trojan-Go-Next，可以直接跳到 [配置入门](/guide/config) 或 [完整配置文件](/guide/full-config)。

## 你需要什么

- 一台境外 VPS（推荐 Ubuntu / Debian）
- 一个域名，A 记录指向 VPS 的 IP
- 5 分钟时间

## 第一步：下载

从 [Release 页面](https://github.com/corevx/trojan-go-next-next/releases) 下载对应平台的压缩包并解压：

```shell
# Linux amd64 示例
wget https://github.com/corevx/trojan-go-next-next/releases/latest/download/trojan-go-next-linux-amd64.zip
unzip trojan-go-next-linux-amd64.zip
chmod +x trojan-go-next
```

::: tip 下载不了？
如果在大陆无法访问 GitHub，可以使用代理或者从镜像站下载。
:::

## 第二步：申请证书

```shell
sudo apt install certbot
sudo certbot certonly --standalone -d your-domain.com
```

证书会保存在 `/etc/letsencrypt/live/your-domain.com/`。

## 第三步：启动服务端

一条命令启动：

```shell
sudo ./trojan-go-next -server \
    -remote 127.0.0.1:80 \
    -local 0.0.0.0:443 \
    -key /etc/letsencrypt/live/your-domain.com/privkey.pem \
    -cert /etc/letsencrypt/live/your-domain.com/fullchain.pem \
    -password your-password
```

::: warning
把 `your-domain.com` 和 `your-password` 替换成你自己的值。密码请使用强随机字符串。
:::

## 第四步：启动客户端

在你本地电脑上：

```shell
./trojan-go-next -client \
    -remote your-domain.com:443 \
    -local 127.0.0.1:1080 \
    -password your-password
```

## 第五步：验证

```shell
curl --socks5 127.0.0.1:1080 https://www.google.com -I
```

返回 `HTTP 200` 就说明代理已经正常工作了。

## 接下来

- [配置文件模式](/guide/config) — 更灵活的配置方式
- [部署为系统服务](/deployment/systemd) — 开机自启、自动重启
- [Docker 部署](/deployment/docker) — 容器化部署
- [常见问题](/guide/faq) — 遇到问题先看这里
