---
title: "健康检查与监控（v0.11.0 新增）"
---

Trojan-Go-Next v0.11.0 新增了独立的监控服务，提供健康检查端点和 Prometheus 格式的指标输出，方便与 Kubernetes、Prometheus 等运维工具集成。

## 配置

在配置文件中添加 `monitor` 配置块：

```json
{
    "monitor": {
        "enabled": true,
        "monitor_addr": "0.0.0.0",
        "monitor_port": 9090
    }
}
```

或使用 YAML 格式：

```yaml
monitor:
  enabled: true
  monitor-addr: 0.0.0.0
  monitor-port: 9090
```

### 配置项说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用监控服务 |
| `monitor_addr` | string | 监听地址 |
| `monitor_port` | int | 监听端口 |

## 端点

### 存活探针 `/healthz`

```shell
GET /healthz
```

只要进程在运行就返回 `200 OK`，用于判断进程是否存活。

```shell
curl http://127.0.0.1:9090/healthz
# 返回: ok
```

### 就绪探针 `/readyz`

```shell
GET /readyz
```

检查服务是否就绪（transport listener 是否已接受连接）。就绪返回 `200 OK`，未就绪返回 `503 Service Unavailable`。

```shell
curl http://127.0.0.1:9090/readyz
# 就绪: ok
# 未就绪: not ready
```

### Prometheus 指标 `/metrics`

```shell
GET /metrics
```

输出 Prometheus 文本格式的指标数据：

```
# HELP trojan_connections_total Total number of accepted connections
# TYPE trojan_connections_total counter
trojan_connections_total 1584

# HELP trojan_active_connections Currently active connections
# TYPE trojan_active_connections gauge
trojan_active_connections 12

# HELP trojan_connection_duration_seconds Connection duration in seconds
# TYPE trojan_connection_duration_seconds histogram
trojan_connection_duration_seconds_bucket{le="1"} 120
trojan_connection_duration_seconds_bucket{le="5"} 450
trojan_connection_duration_seconds_bucket{le="10"} 780
trojan_connection_duration_seconds_bucket{le="30"} 1200
trojan_connection_duration_seconds_bucket{le="60"} 1400
trojan_connection_duration_seconds_bucket{le="+Inf"} 1584
trojan_connection_duration_seconds_count 1584
```

## Prometheus 集成

在 `prometheus.yml` 中添加 scrape 配置：

```yaml
scrape_configs:
  - job_name: 'trojan-go-next'
    static_configs:
      - targets: ['127.0.0.1:9090']
    scrape_interval: 15s
```

## Kubernetes 集成

在 Pod 或 Deployment 中配置探针：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: trojan-go-next
spec:
  template:
    spec:
      containers:
        - name: trojan-go-next
          image: ghcr.io/corevx/trojan-go-next-next
          ports:
            - containerPort: 443
            - containerPort: 9090
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

## 内置指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `trojan_connections_total` | Counter | 累计接受的连接数 |
| `trojan_active_connections` | Gauge | 当前活跃连接数 |
| `trojan_connection_duration_seconds` | Histogram | 连接持续时间（秒） |
| `trojan_accept_errors_total` | Counter | 接受连接时的错误次数 |
