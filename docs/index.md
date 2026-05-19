---
layout: home

hero:
  name: Trojan-Go-Next
  text: 安全、高效、易用的 Trojan 代理
  tagline: 使用 Go 实现的完整 Trojan 代理，兼容原版 Trojan 协议与配置文件格式
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/install
    - theme: alt
      text: 配置入门
      link: /guide/config
    - theme: alt
      text: GitHub
      link: https://github.com/corevx/trojan-go-next-next

features:
  - icon: 🔒
    title: TLS 隧道传输
    details: 流量特征与 HTTPS 完全一致，基于成熟的 TLS 加密体系，对抗 GFW 被动/主动检测
  - icon: ⚡
    title: 多路复用
    details: 基于 smux 的连接复用，减少 TLS 握手开销，降低延迟、提升并发性能
  - icon: 🌐
    title: WebSocket + CDN 中转
    details: 支持 CDN 流量中转和对抗中间人攻击，同时兼容非 WebSocket 客户端
  - icon: 🛡️
    title: AEAD 二次加密
    details: Shadowsocks AEAD 加密层，防止不可信 CDN 识别和审查流量
  - icon: 🔀
    title: 路由分流
    details: 内建 GeoIP/GeoSite 路由模块，支持国内直连、海外代理、广告屏蔽
  - icon: 📡
    title: REST API + 监控
    details: HTTP REST API 动态管理用户，健康检查端点，Prometheus 指标输出（v0.11.0 新增）
  - icon: 🔐
    title: TLS 安全加固
    details: 证书有效期检测与告警、最低 TLS 版本控制、SNI 校验（v0.11.0 新增）
  - icon: 📦
    title: 跨平台零依赖
    details: 单二进制文件，支持 Linux / macOS / Windows / MIPS 路由器，Docker 镜像一键部署
---

## 与其他代理工具对比

| 特性 | Trojan-Go-Next | 原版 Trojan | V2Ray / Xray | Clash | sing-box |
|------|:---------:|:-----------:|:------------:|:-----:|:--------:|
| Trojan 协议 | ✅ 兼容原版 | ✅ | ✅ | ✅ | ✅ |
| WebSocket CDN 中转 | ✅ | ❌ | ✅ | ✅ | ✅ |
| 多路复用 (smux) | ✅ | ❌ | ✅ | ❌ | ❌ |
| AEAD 二次加密 | ✅ | ❌ | ❌ | ❌ | ❌ |
| 路由分流 (GeoIP/Site) | ✅ | ❌ | ✅ | ✅ | ✅ |
| 透明代理 (TProxy) | ✅ | ✅ | ✅ | ❌ | ✅ |
| REST API | ✅ | ❌ | ❌ | ✅ | ✅ |
| Prometheus 指标 | ✅ | ❌ | ❌ | ❌ | ❌ |
| 证书到期检测/告警 | ✅ | ❌ | ❌ | ❌ | ❌ |
| 单二进制零依赖 | ✅ | ✅ | ❌ | ❌ | ✅ |
| YAML 配置 | ✅ | ❌ | ✅ | ✅ | ✅ |
| MySQL 用户认证 | ✅ | ✅ | ❌ | ❌ | ❌ |
| 可插拔传输层 | ✅ | ❌ | ✅ | ❌ | ✅ |
| Docker 镜像 | ✅ | ❌ | ✅ | ✅ | ✅ |

## 快速开始

### 服务端

```shell
sudo ./trojan-go-next -server -remote 127.0.0.1:80 -local 0.0.0.0:443 \
    -key ./your_key.key -cert ./your_cert.crt -password your_password
```

### 客户端

```shell
./trojan-go-next -client -remote example.com:443 -local 127.0.0.1:1080 \
    -password your_password
```

### Docker 部署

```shell
docker run --name trojan-go-next -d \
    -v /etc/trojan-go-next/:/etc/trojan-go-next \
    --network host \
    ghcr.io/corevx/trojan-go-next-next
```

## 兼容的客户端

Trojan-Go-Next 服务端兼容所有支持标准 Trojan 协议的客户端：

- [v2rayN](https://github.com/2dust/v2rayN) — Windows / macOS / Linux
- [Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev) — 跨平台
- [NekoBox for Android](https://github.com/MatsuriDayo/NekoBoxForAndroid) — Android
- [ShadowRocket](https://apps.apple.com/app/shadowrocket/id932747118) — iOS
- [sing-box](https://github.com/SagerNet/sing-box) — 通用代理平台

> **注意：** 以上客户端支持标准 Trojan 协议。Trojan-Go-Next 扩展特性（WebSocket、多路复用、AEAD）需要直接运行 `trojan-go-next` 二进制文件。

## 社区

如遇到配置和使用问题、发现 bug，或是有更好的想法，欢迎加入 [Telegram 交流反馈群](https://t.me/trojan_go_chat)。

> Across the Great Wall, we can reach every corner in the world.
>
> (越过长城，走向世界。)
