---
title: "ShadowTLS v3 抗探测"
---

# ShadowTLS v3 抗探测

ShadowTLS 让代理端口在探测者眼中与一个真实的 HTTPS 站点**无法区分**：未授权连接（包括主动探测）会被完整中继到真实站点，探测者看到的是真站的真实 TLS 证书与内容；只有携带正确 HMAC 签名的客户端才能进入代理通道。

**最大优势：无需自有域名、无需 TLS 证书**，且主流客户端（sing-box、mihomo/Clash.Meta 系）原生支持，零定制客户端即可连接。

## 工作原理

1. 客户端在 TLS ClientHello 的 `session_id` 字段嵌入密码的 HMAC 签名
2. 服务端验签：
   - **验签失败**（探测者/无效客户端）：整个握手过程透明中继到 `handshake` 指定的真实站点——探测者与真站完成一次货真价实的 TLS 握手，拿到真站证书
   - **验签成功**：服务端继续中继真站握手以保持外观，同时用与 ServerRandom 绑定的 HMAC/XOR 密钥劫持后续数据帧，内层跑裸 trojan 协议
3. strict 模式（默认开启）要求真站握手必须是 TLS 1.3，进一步收紧伪装一致性

## 服务端配置

```json
{
  "run_type": "server",
  "local_addr": "0.0.0.0",
  "local_port": 443,
  "password": ["你的trojan密码"],
  "disable_http_check": true,
  "shadowtls": {
    "enabled": true,
    "version": 3,
    "users": [{"name": "user1", "password": "shadowtls密码"}],
    "handshake": {"server": "www.apple.com", "server_port": 443},
    "strict": true
  }
}
```

| 字段 | 说明 |
|------|------|
| `version` | 协议版本，当前仅支持 `3` |
| `users` | ShadowTLS 层的用户列表（与 trojan `password` 是两层独立认证） |
| `handshake` | 真实站点地址，建议选择支持 TLS 1.3 的大站（如 `www.apple.com:443`） |
| `strict` | 严格模式：真站握手非 TLS 1.3 时拒绝提供代理服务（默认 `true`） |

::: warning 注意
- **该端口不得再开启 `ssl`**——外层 TLS 已由真站握手中继提供，内层是裸 trojan，重复套 TLS 会导致客户端无法连接
- **不可与 `websocket`、`transport_plugin` 组合使用**
- 真站故障时服务不可用（握手无处中继），选一个稳定的大站
:::

## 客户端配置（无需本软件）

**mihomo / Clash.Meta：**

```yaml
proxies:
  - name: stls3-trojan
    type: trojan
    server: 服务器地址
    port: 443
    password: 你的trojan密码
    sni: www.apple.com          # 与服务端 handshake 真站一致
    skip-cert-verify: true
    client-fingerprint: chrome
    shadow-tls-opts:
      version: 3
      password: shadowtls密码
```

**sing-box：**

```json
{
  "outbounds": [
    {
      "type": "shadowtls",
      "tag": "st-out",
      "server": "服务器地址",
      "server_port": 443,
      "version": 3,
      "password": "shadowtls密码",
      "tls": {"enabled": true, "server_name": "www.apple.com", "insecure": true,
               "utls": {"enabled": true, "fingerprint": "chrome"}}
    },
    {
      "type": "trojan",
      "tag": "proxy-out",
      "server": "服务器地址",
      "server_port": 443,
      "password": "你的trojan密码",
      "detour": "st-out"
    }
  ]
}
```

## 与 nginx SNI 分流共存

单 IP 多服务时，在 nginx stream 层按 SNI 分流，与现有 trojan-go 部署方式一致：

```nginx
stream {
    map $ssl_preread_server_name $backend {
        www.apple.com   trojan_go_shadowtls;   # SNI 为真站域名 → shadowtls 端口
        其他域名         其他服务;
    }
    server {
        listen 443;
        ssl_preread on;
        proxy_pass $backend;
    }
    upstream trojan_go_shadowtls {
        server 127.0.0.1:9443;   # trojan-go shadowtls 监听端口
    }
}
```

## 实测参考

v1.3.0 发布前完成三层验证：mihomo v1.19.33 与 sing-box v1.14.3 真实客户端端到端（HTTP 200，含 10 路并发）；openssl/curl 直连探测只见到真站证书；跨机公网部署吞吐为直连基准的约 68%（双加密正常损耗）。
