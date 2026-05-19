---
title: "结构化日志（v0.11.0 新增）"
---

Trojan-Go-Next v0.11.0 新增了结构化日志支持，可以在日志输出中附加键值对字段，方便日志采集和检索。

## 基本用法

### WithField / WithFields

```go
import "github.com/p4gefau1t/trojan-go-next/log"

// 单个字段
log.WithField("user", hash).Info("connection closed")

// 多个字段
log.WithFields(log.Fields{
    "user":   hash,
    "remote": addr,
}).Info("connection closed")
```

文本输出格式：

```
[INFO] connection closed user=abc remote=1.2.3.4:1234
```

### 链式调用

```go
log.WithField("user", hash).
    WithField("remote", addr).
    WithField("duration", time.Since(start)).
    Info("session ended")
```

输出：

```
[INFO] session ended user=abc remote=1.2.3.4:1234 duration=5.234s
```

### 支持的日志级别

`Entry` 支持所有标准日志级别：

```go
entry := log.WithField("request_id", id)

entry.Trace("debug detail")
entry.Debug("debug message")
entry.Info("information")
entry.Warn("warning")
entry.Error("error occurred")
entry.Fatal("fatal error") // 会调用 os.Exit(1)
```

也支持格式化版本：

```go
log.WithField("port", 443).Infof("listening on port %d", 443)
```

## 连接追踪

v0.11.0 引入了连接 ID（`ConnID`），为每个连接分配唯一标识，贯穿隧道栈：

```go
import (
    "github.com/p4gefau1t/trojan-go-next/common"
    "github.com/p4gefau1t/trojan-go-next/log"
)

connID := common.NewConnID()
log.WithField("conn_id", connID).Info("new connection")

// ... 连接处理 ...

duration := time.Since(startTime)
log.WithField("conn_id", connID).WithField("duration", duration).Info("connection closed")
```

### Context 传播

ConnID 可通过 `context.Context` 在隧道栈中传播：

```go
ctx := common.ContextWithConnID(context.Background(), connID)

// 在其他层中取出
id := common.ConnIDFromContext(ctx)
```

## 与现有日志兼容

所有现有的 `log.Info()`、`log.Error()` 等调用不受影响，无需修改。结构化日志是增量添加的能力。
