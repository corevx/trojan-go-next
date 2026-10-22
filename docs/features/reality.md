---
title: "REALITY 出站"
---

# REALITY 出站

客户端 REALITY 支持（v1.4.0）：以浏览器指纹的 TLS ClientHello 连接 [REALITY](https://github.com/XTLS/REALITY) 服务端，认证凭证密封在 ClientHello 的 `session_id` 字段中（x25519 ECDH + HKDF 派生）。对中间人而言，流量与访问伪装真站的普通浏览器 TLS 连接无法区分。

本实现为**出站（客户端）方向**，服务端使用 sing-box / mihomo 的 trojan-REALITY 入站即可对接（Xray 系的 VLESS 入站不适用）。

## 客户端配置

```json
{
  "run_type": "client",
  "local_addr": "127.0.0.1",
  "local_port": 1080,
  "remote_addr": "服务器地址",
  "remote_port": 443,
  "password": ["trojan密码"],
  "reality": {
    "enabled": true,
    "public-key": "服务端下发的 x25519 公钥",
    "short-id": "服务端配置的 short_id",
    "server-name": "www.apple.com",
    "fingerprint": "chrome"
  }
}
```

| 字段 | 说明 |
|------|------|
| `public-key` | 服务端 REALITY 公钥（`sing-box generate reality-keypair` 或 `xray x25519` 生成，由服务端下发） |
| `short-id` | 服务端配置的 short_id 之一（十六进制字符串） |
| `server-name` | 伪装真站域名，须与服务端 `handshake` 目标一致 |
| `fingerprint` | TLS 指纹：`chrome`（默认）/ `firefox` / `ios` |

凭证错误时服务端会透明回落到伪装真站，客户端因证书校验失败而报错——不会误连，也不会暴露代理特征。

::: warning 组合限制
REALITY 与 `websocket`、`transport_plugin` 互斥（REALITY 自带传输伪装，叠加 WS 会破坏指纹一致性），配置校验会直接拒绝该组合。
:::

## 对接的服务端（sing-box 示例）

```json
{
  "inbounds": [{
    "type": "trojan",
    "listen": "0.0.0.0",
    "listen_port": 443,
    "users": [{"name": "u1", "password": "trojan密码"}],
    "tls": {
      "enabled": true,
      "server_name": "www.apple.com",
      "reality": {
        "enabled": true,
        "handshake": {"server": "www.apple.com", "server_port": 443},
        "private_key": "服务端私钥",
        "short_id": ["与服务端客户端一致的short_id"]
      }
    }
  }],
  "outbounds": [{"type": "direct", "tag": "direct-out"}],
  "route": {"final": "direct-out"}
}
```

## 实测参考

v1.4.0 发布前实测：对 sing-box v1.14.3 的 trojan-REALITY 入站端到端代理成功（4 路并发往返），错误 `short-id` 与错误 `public-key` 均被正确拒绝；linux 容器与跨机公网环境均验证通过。实现零新增依赖（utls + Go 标准库）。
