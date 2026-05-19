---
title: "指标系统（v0.11.0 新增）"
---

Trojan-Go v0.11.0 引入了轻量级的指标采集系统，基于 `sync/atomic` 实现，零外部依赖。

## 指标类型

### Counter（计数器）

单调递增的计数器，用于统计连接总数、错误总数等：

```go
import "github.com/p4gefau1t/trojan-go/metric"

connTotal := metric.RegisterCounter(
    "trojan_connections_total",
    "Total number of accepted connections",
)

connTotal.Inc()      // +1
connTotal.Add(10)    // +10
val := connTotal.Value() // 读取当前值
```

### Gauge（仪表盘）

可增减的实时值，用于统计活跃连接数等：

```go
activeConns := metric.RegisterGauge(
    "trojan_active_connections",
    "Currently active connections",
)

activeConns.Inc()   // +1（新连接）
activeConns.Dec()   // -1（连接关闭）
activeConns.Set(0)  // 重置
val := activeConns.Value()
```

### Histogram（直方图）

观察值分布统计，用于分析连接持续时间等：

```go
duration := metric.RegisterHistogram(
    "trojan_connection_duration_seconds",
    "Connection duration in seconds",
    []float64{1, 5, 10, 30, 60},  // 分桶边界
)

duration.Observe(3.5) // 记录一个 3.5 秒的连接

buckets := duration.Buckets() // [1, 5, 10, 30, 60]
counts  := duration.Counts()  // 各桶的计数
total   := duration.Total()   // 总观察次数
```

## 全局注册表

```go
reg := metric.Default()

// 注册指标
counter := metric.RegisterCounter("my_counter", "help text")
gauge   := metric.RegisterGauge("my_gauge", "help text")
hist    := metric.RegisterHistogram("my_hist", "help text", []float64{1, 5, 10})

// 查找已注册的指标
c := reg.Counter("my_counter")
g := reg.Gauge("my_gauge")
h := reg.Histogram("my_hist")
```

重复注册同一名称会返回已有实例，不会重复创建。

## Prometheus 输出

使用 `WritePrometheus` 将所有注册指标输出为 Prometheus 文本格式：

```go
import (
    "net/http"
    "github.com/p4gefau1t/trojan-go/metric"
)

http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/plain; version=0.0.4")
    metric.WritePrometheus(w, metric.Default())
})
```

输出示例：

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
trojan_connection_duration_seconds_bucket{le="+Inf"} 1584
trojan_connection_duration_seconds_count 1584
```

## 线程安全

所有指标操作基于 `sync/atomic`，无需额外加锁。Counter 使用 `atomic.AddUint64`，Gauge 使用 `atomic.AddInt64` / `atomic.StoreInt64`，Histogram 内部各桶独立原子计数。可以在多个 goroutine 中安全使用。

## 内置指标

Trojan-Go 内部注册了以下指标：

| 名称 | 类型 | 位置 |
|------|------|------|
| `trojan_connections_total` | Counter | `tunnel/trojan/server.go` |
| `trojan_active_connections` | Gauge | `proxy/proxy.go` |
| `trojan_connection_duration_seconds` | Histogram | `tunnel/trojan/server.go` |
| `trojan_accept_errors_total` | Counter | `tunnel/transport/server.go` |
