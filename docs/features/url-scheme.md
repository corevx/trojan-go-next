---
title: URL 分享链接
---

# URL 分享链接

Trojan-Go 支持通过 URL 分享链接快速导入配置，兼容标准 `trojan://` 格式并扩展了 `trojan-go://` 格式。

## 基本格式

```
trojan-go://password@host:port/?参数
```

## 使用方式

```shell
# 直接通过 URL 启动客户端
./trojan-go -url 'trojan-go://password@example.com:443/'

# 附加选项
./trojan-go -url 'trojan-go://password@example.com:443/' -url-option 'mux=true'
```

## 参数说明

| 参数 | 说明 | 示例 |
|------|------|------|
| `sni` | TLS SNI | `sni=example.com` |
| `type` | 传输类型 | `type=ws`（WebSocket） |
| `host` | WebSocket Host | `host=cdn.example.com` |
| `path` | WebSocket 路径 | `path=%2Fws-path` |
| `encryption` | AEAD 加密 | `encryption=ss;AES-128-GCM:password` |
| `plugin` | 传输插件 | `plugin=obfs-local` |

## 示例

### 标准 Trojan 链接

```
trojan://password@example.com:443?sni=example.com
```

### WebSocket + CDN

```
trojan-go://password@cdn.example.com:443/?type=ws&path=%2Fws&host=example.com
```

### 启用 AEAD 加密

```
trojan-go://password@example.com:443/?encryption=ss;AES-128-GCM:encryption-password
```

## 兼容性

- `trojan://` 格式完全兼容标准 Trojan 客户端
- `trojan-go://` 扩展格式仅 Trojan-Go 客户端支持
- 所有 Trojan-Go 扩展参数在不支持时会被忽略

::: tip
URL 中的密码和特殊字符需要 URL 编码。
:::

## 技术规范

详细的 URL 方案规范参见 [URL 方案规范（开发者）](/developer/url-spec)。
