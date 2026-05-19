---
title: Prometheus 指标
---

# Prometheus 指标

Trojan-Go v0.11.0 内置 Prometheus 指标输出，可与 Grafana 等可视化工具集成。

## 启用监控服务

```json
{
    "monitor": {
        "enabled": true,
        "monitor_addr": "0.0.0.0",
        "monitor_port": 9090
    }
}
```

启用后可通过以下端点访问：

| 端点 | 说明 |
|------|------|
| `GET /metrics` | Prometheus 格式指标输出 |
| `GET /healthz` | 存活检查（始终返回 200） |
| `GET /readyz` | 就绪检查（服务就绪后返回 200） |

## 内置指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `trojan_active_connections` | Gauge | 当前活跃连接数 |
| `trojan_connections_total` | Counter | 总连接数 |

## 配置 Prometheus

在 `prometheus.yml` 中添加：

```yaml
scrape_configs:
  - job_name: 'trojan-go'
    static_configs:
      - targets: ['your-server:9090']
    scrape_interval: 15s
```

## 配置 Grafana

推荐监控面板指标：

- **活跃连接数**：`trojan_active_connections` — 实时连接数趋势
- **连接速率**：`rate(trojan_connections_total[5m])` — 每秒新建连接数
- **总连接数**：`trojan_connections_total` — 累计连接数

## Kubernetes 集成

如果使用 Kubernetes 部署，可以配置 liveness 和 readiness 探针：

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 9090
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 9090
  initialDelaySeconds: 5
  periodSeconds: 10
```

## 安全建议

- 监控端口不应暴露到公网，建议监听 `127.0.0.1` 或通过防火墙限制访问
- Prometheus 指标中不包含用户密码或连接内容等敏感信息
- 如需通过公网访问，建议配置反向代理并启用 TLS
