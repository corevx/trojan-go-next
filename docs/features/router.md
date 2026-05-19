---
title: "路由分流与广告屏蔽"
---

# 路由分流与广告屏蔽

## 功能概述

Trojan-Go 内建路由模块，基于 GeoIP 和 GeoSite 数据库实现流量分流。客户端可对每个连接应用三种策略：

| 策略 | 行为 |
|------|------|
| `proxy` | 通过 Trojan 代理转发 |
| `bypass` | 直连，不经过代理 |
| `block` | 拒绝连接 |

服务端仅支持 `block` 策略（用于屏蔽恶意请求）。

## 规则格式

路由规则以字符串数组形式配置，每条规则由前缀和匹配内容组成：

| 前缀 | 匹配方式 | 示例 | 说明 |
|------|---------|------|------|
| `domain:` | 子域名匹配 | `domain:google.com` | 匹配 `google.com` 及其所有子域名 |
| `full:` | 完全匹配 | `full:www.google.com` | 仅匹配 `www.google.com`，不匹配其他子域名 |
| `regexp:` | 正则表达式 | `regexp:.*\.google\.com$` | 使用正则匹配域名 |
| `cidr:` | CIDR 网段 | `cidr:192.168.0.0/16` | 匹配指定 IP 网段 |
| `geoip:` | GeoIP 数据库 | `geoip:cn` | 匹配中国 IP 段 |
| `geosite:` | GeoSite 数据库 | `geosite:cn` | 匹配中国域名列表 |

### GeoIP 国家代码

使用两位字母的国家/地区代码，如 `cn`（中国）、`us`（美国）、`jp`（日本）、`hk`（香港）。特殊代码 `private` 匹配内网和保留 IP 地址。完整代码参考 [ISO 3166-1](https://zh.wikipedia.org/wiki/ISO_3166-1)。

### GeoSite 标签

使用 V2Ray 社区维护的域名分类标签。常用标签：

- `cn` -- 中国大陆域名
- `geolocation-cn` -- 地理定位在中国的域名
- `geolocation-!cn` -- 地理定位不在中国大陆的域名
- `category-ads` -- 广告域名
- `category-ads-all` -- 所有广告域名（更全面）
- `google`、`github`、`bilibili` 等 -- 特定服务域名

所有可用标签可在 [domain-list-community](https://github.com/v2fly/domain-list-community/tree/master/data) 仓库的 `data` 目录中查阅。

## 完整配置示例

以下配置实现：国内流量直连 + 广告屏蔽 + 其余走代理。

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "your-server.com",
    "remote_port": 443,
    "password": [
        "your-strong-password"
    ],
    "router": {
        "enabled": true,
        "bypass": [
            "geoip:cn",
            "geoip:private",
            "geosite:cn",
            "geosite:geolocation-cn"
        ],
        "block": [
            "geosite:category-ads-all"
        ],
        "proxy": [
            "geosite:geolocation-!cn"
        ]
    }
}
```

### 自定义规则示例

屏蔽特定域名和 IP 段：

```json
"block": [
    "domain:ad.example.com",
    "full:tracker.bad-site.com",
    "regexp:.*adservice\\..*",
    "cidr:10.0.0.0/8"
]
```

## DNS 策略配置

`domain_strategy` 控制路由模块如何解析域名，影响匹配行为和 DNS 请求走向：

```json
"router": {
    "enabled": true,
    "domain_strategy": "as_is",
    "bypass": ["geoip:cn"],
    "block": ["geosite:category-ads-all"],
    "proxy": ["geosite:geolocation-!cn"]
}
```

| 策略 | 行为 | DNS 请求 | 适用场景 |
|------|------|---------|---------|
| `as_is` | 仅用域名匹配规则，不解析 IP | 不发送 | 默认策略，推荐使用 |
| `ip_if_non_match` | 域名规则未匹配时，解析域名再按 IP 匹配 | 本地 DNS | 需要精确分流 |
| `ip_on_demand` | 始终解析域名，同时匹配域名和 IP 规则 | 本地 DNS | 最精确但开销最大 |

::: warning
`ip_if_non_match` 和 `ip_on_demand` 会将域名查询发送到本地 DNS。如果你的本地 DNS 不可信，可能导致 DNS 泄漏，暴露访问记录。默认的 `as_is` 策略不发送 DNS 请求，安全性最高。
:::

## GeoIP / GeoSite 数据文件

路由模块依赖两个数据文件：

| 文件 | 内容 | 来源 |
|------|------|------|
| `geoip.dat` | IP 地址段数据库 | [v2fly/geoip](https://github.com/v2fly/geoip) |
| `geosite.dat` | 域名分类数据库 | [v2fly/domain-list-community](https://github.com/v2fly/domain-list-community) |

### 数据文件位置

Trojan-Go 按以下顺序查找数据文件：

1. 环境变量 `TROJAN_GO_LOCATION_ASSET` 指定的目录
2. Trojan-Go 可执行文件所在目录

Release 压缩包已包含这两个文件，解压后直接可用。如需自定义路径：

```bash
export TROJAN_GO_LOCATION_ASSET=/path/to/asset/directory
trojan-go -config config.json
```

::: tip
GeoIP 和 GeoSite 数据库需要定期更新以保持准确性。建议每月更新一次。
:::

## 验证方法

测试国内直连是否生效（通过 SOCKS5 代理访问国内站点，应不经由远程服务器）：

```bash
# 访问国内站点，观察响应时间和 IP
curl -x socks5://127.0.0.1:1080 -s \
     -w "Total: %{time_total}s\nRemote IP: %{remote_ip}\n" \
     -o /dev/null \
     https://www.baidu.com
```

如果路由配置正确，访问国内站点的延迟应远低于通过远程服务器中转的延迟。可通过对比直接访问和代理访问的耗时来判断：

```bash
# 直接访问（不走代理）
curl -s -w "Direct: %{time_total}s\n" -o /dev/null https://www.baidu.com

# 通过代理访问（国内站点应直连）
curl -x socks5://127.0.0.1:1080 -s -w "Proxy: %{time_total}s\n" -o /dev/null https://www.baidu.com
```

两者的耗时应该接近，说明国内流量成功绕过了代理。

以 debug 模式启动客户端可查看路由决策日志：

```bash
trojan-go -config config.json -log debug
```

日志中会显示每个连接的路由决策结果（`proxy` / `bypass` / `block`）。
