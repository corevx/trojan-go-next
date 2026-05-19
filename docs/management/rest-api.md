---
title: "REST API（v0.11.0 新增）"
---

Trojan-Go v0.11.0 新增了基于 HTTP 的 REST API，作为 gRPC API 的补充，提供更简洁的用户管理接口。

## 配置

在服务端配置文件中添加 `rest` 配置块：

```json
{
    "api": {
        "enabled": true,
        "api_addr": "127.0.0.1",
        "api_port": 10000
    },
    "rest": {
        "enabled": true,
        "rest_port": 10001,
        "api_key": "your-secret-key",
        "cors": ["https://your-dashboard.example.com"]
    }
}
```

或使用 YAML 格式：

```yaml
api:
  enabled: true
  api-addr: 127.0.0.1
  api-port: 10000
rest:
  enabled: true
  rest-port: 10001
  api-key: your-secret-key
  cors:
    - https://your-dashboard.example.com
```

### 配置项说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 REST API |
| `rest_port` | int | REST API 监听端口 |
| `api_key` | string | API 认证密钥，为空则不进行认证 |
| `cors` | []string | 允许的跨域来源列表 |

## 认证

当配置了 `api_key` 后，所有请求必须在 HTTP 头中携带 `X-API-Key`：

```shell
curl -H "X-API-Key: your-secret-key" http://127.0.0.1:10001/api/v1/users
```

未携带或密钥错误将返回 `401 Unauthorized`。

## API 端点

### 列出所有用户

```shell
GET /api/v1/users
```

响应示例：

```json
{
  "users": [
    {
      "hash": "d63dc919e201d7bc4c825630d2cf25fdc93d4b2f0d46706d29038d01",
      "sent": 36393,
      "recv": 186478,
      "speed_sent": 25210,
      "speed_recv": 72384,
      "ip_count": 2,
      "ip_limit": 50
    }
  ]
}
```

### 查询单个用户

```shell
GET /api/v1/users/{hash}
```

响应示例：

```json
{
  "hash": "d63dc919e201d7bc4c825630d2cf25fdc93d4b2f0d46706d29038d01",
  "sent": 36393,
  "recv": 186478,
  "speed_sent": 25210,
  "speed_recv": 72384,
  "speed_limit": { "up": 5242880, "down": 5242880 },
  "ip_count": 2,
  "ip_limit": 50
}
```

### 添加用户

```shell
POST /api/v1/users
Content-Type: application/json

{
  "hash": "d63dc919e201d7bc4c825630d2cf25fdc93d4b2f0d46706d29038d01",
  "speed_up": 5242880,
  "speed_down": 5242880,
  "ip_limit": 5
}
```

成功响应：`201 Created`

```json
{ "status": "created" }
```

### 修改用户

```shell
PUT /api/v1/users/{hash}
Content-Type: application/json

{
  "speed_up": 10485760,
  "speed_down": 10485760,
  "ip_limit": 3
}
```

成功响应：

```json
{ "status": "updated" }
```

### 删除用户

```shell
DELETE /api/v1/users/{hash}
```

成功响应：

```json
{ "status": "deleted" }
```

### 查询流量

```shell
GET /api/v1/traffic/{hash}
```

响应示例：

```json
{
  "hash": "d63dc919e201d7bc4c825630d2cf25fdc93d4b2f0d46706d29038d01",
  "sent": 36393,
  "recv": 186478,
  "speed_sent": 25210,
  "speed_recv": 72384
}
```

### 全局统计

```shell
GET /api/v1/stats
```

响应示例：

```json
{
  "active_connections": 12,
  "total_connections": 1584,
  "users": 5,
  "total_sent": 1073741824,
  "total_recv": 2147483648
}
```

## 安全特性

- **API Key 认证**：通过 `X-API-Key` 请求头进行认证
- **CORS 控制**：可配置允许的跨域来源
- **速率限制**：内置 100 RPS 限流保护
- **请求日志**：每个请求记录方法、路径、耗时和来源地址

## 完整示例

```shell
# 添加用户
curl -X POST http://127.0.0.1:10001/api/v1/users \
  -H "X-API-Key: your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"hash":"d63dc919...","speed_up":5242880,"speed_down":5242880,"ip_limit":5}'

# 查询所有用户
curl -H "X-API-Key: your-secret-key" http://127.0.0.1:10001/api/v1/users

# 修改用户限速
curl -X PUT http://127.0.0.1:10001/api/v1/users/d63dc919... \
  -H "X-API-Key: your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"speed_up":10485760,"speed_down":10485760,"ip_limit":3}'

# 查看全局统计
curl -H "X-API-Key: your-secret-key" http://127.0.0.1:10001/api/v1/stats

# 删除用户
curl -X DELETE -H "X-API-Key: your-secret-key" \
  http://127.0.0.1:10001/api/v1/users/d63dc919...
```
