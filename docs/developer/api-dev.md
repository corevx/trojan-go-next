---
title: "API 开发"
---

# API 开发

Trojan-Go-Next 通过 gRPC + Protobuf 提供 API 服务，支持流量统计、用户管理和速度限制等功能。API 定义位于 `api/service/api.proto`。

## API 概览

| 服务 | 说明 | 包含标签 |
|------|------|----------|
| `TrojanClientService` | 客户端 API：获取当前流量和速度 | `api` 或 `full` |
| `TrojanServerService` | 服务端 API：用户管理、流量统计、限速 | `api` 或 `full` |

API 模块还包含以下辅助组件（同样需要 `api` 标签）：

- `api/control` — 命令行管理工具（`trojan-go-next -api ...`）
- `api/monitor` — Prometheus 监控指标
- `api/rest` — REST API 网关

## Proto 服务定义

### TrojanClientService

客户端服务，仅提供流量查询：

```protobuf
service TrojanClientService {
    // 获取当前用户的流量和速度
    rpc GetTraffic(GetTrafficRequest) returns(GetTrafficResponse){}
}
```

**GetTraffic** 请求/响应：

| 字段 | 类型 | 说明 |
|------|------|------|
| `request.user.password` | string | 用户明文密码 |
| `request.user.hash` | string | 用户密码哈希 |
| `response.traffic_total` | Traffic | 累计上传/下载流量 |
| `response.speed_current` | Speed | 当前上传/下载速度 |

### TrojanServerService

服务端服务，提供完整的用户管理能力：

```protobuf
service TrojanServerService {
    // 列出所有用户及其状态
    rpc ListUsers(ListUsersRequest) returns(stream ListUsersResponse){}

    // 查询指定用户的详细信息（支持批量流式请求）
    rpc GetUsers(stream GetUsersRequest) returns(stream GetUsersResponse){}

    // 增删改用户（支持批量流式操作）
    rpc SetUsers(stream SetUsersRequest) returns(stream SetUsersResponse){}
}
```

## 核心 RPC 方法

### ListUsers — 列出全部用户

服务端流式 RPC，返回所有用户的状态信息：

```protobuf
message ListUsersResponse {
    UserStatus status = 1;
}
```

每个 `UserStatus` 包含：

| 字段 | 类型 | 说明 |
|------|------|------|
| `user` | User | 用户信息（password + hash） |
| `traffic_total` | Traffic | 累计上传/下载流量（字节） |
| `speed_current` | Speed | 当前上传/下载速度（字节/秒） |
| `speed_limit` | Speed | 上传/下载速度上限 |
| `ip_current` | int32 | 当前在线 IP 数 |
| `ip_limit` | int32 | 最大允许 IP 数 |

### GetUsers — 查询用户详情

双向流式 RPC，可批量查询多个用户：

```protobuf
message GetUsersRequest {
    User user = 1;  // 通过 password 或 hash 指定用户
}

message GetUsersResponse {
    bool success = 1;
    string info = 2;
    UserStatus status = 3;
}
```

### SetUsers — 用户增删改

双向流式 RPC，通过 `Operation` 枚举区分操作类型：

```protobuf
message SetUsersRequest {
    enum Operation {
        Add = 0;      // 添加用户
        Delete = 1;   // 删除用户
        Modify = 2;   // 修改用户属性
    }
    UserStatus status = 1;
    Operation operation = 2;
}

message SetUsersResponse {
    bool success = 1;
    string info = 2;  // 失败时包含错误信息
}
```

操作示例：

| 操作 | 需要设置的字段 |
|------|---------------|
| Add | `status.user.hash`（密码哈希）+ `status.speed_limit` + `status.ip_limit` |
| Delete | `status.user.hash` 或 `status.user.password` |
| Modify | `status.user.hash` + 需要修改的字段（`speed_limit`、`ip_limit`） |

## 配置

在配置文件中添加 `api` 段启用 API：

```json
{
  "api": {
    "enabled": true,
    "api_addr": "127.0.0.1",
    "api_port": 10000,
    "ssl": {
      "enabled": true,
      "cert": "server.crt",
      "key": "server.key",
      "verify_client": true,
      "client_cert": [
        "client1.crt",
        "client2.crt"
      ]
    }
  }
}
```

YAML 格式等价配置：

```yaml
api:
  enabled: true
  api-addr: 127.0.0.1
  api-port: 10000
  ssl:
    enabled: true
    cert: server.crt
    key: server.key
    verify-client: true
    client-cert:
      - client1.crt
      - client2.crt
```

### 配置字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | false | 是否启用 API |
| `api_addr` | string | - | API 监听地址 |
| `api_port` | int | - | API 监听端口 |
| `ssl.enabled` | bool | false | 是否启用 TLS |
| `ssl.cert` | string | - | 服务端证书路径 |
| `ssl.key` | string | - | 服务端私钥路径 |
| `ssl.verify_client` | bool | false | 是否验证客户端证书（mTLS） |
| `ssl.client_cert` | []string | - | 受信任的客户端证书列表 |

## 安全警告

::: danger 安全风险
**务必启用 SSL 并配置 mTLS。** API 拥有完整的用户管理权限（增删改查、限速、限制 IP），未加密暴露等于将服务器控制权交给网络上的任何人。

- `api_addr` 应设为 `127.0.0.1`（仅本机可访问），不要设为 `0.0.0.0`
- 生产环境必须启用 `ssl.enabled` + `ssl.verify_client`
- 服务端启动时如果未启用 SSL，会输出警告日志：

```
WARN grpc API running without TLS - consider enabling ssl for security
```
:::

## 使用 grpcurl 与 API 交互

[grpcurl](https://github.com/fullstorydev/grpcurl) 是一个命令行 gRPC 客户端，适合调试和运维使用。

### 列出所有用户

```bash
# 无 TLS
grpcurl -plaintext 127.0.0.1:10000 trojan.api.TrojanServerService/ListUsers

# 有 TLS（跳过证书验证）
grpcurl -insecure 127.0.0.1:10000 trojan.api.TrojanServerService/ListUsers

# mTLS（指定客户端证书）
grpcurl -insecure \
  -cert client.crt -key client.key \
  127.0.0.1:10000 trojan.api.TrojanServerService/ListUsers
```

### 添加用户

```bash
grpcurl -plaintext -d '{
  "status": {
    "user": {
      "hash": "'$(echo -n "mypassword" | openssl dgst -sha224 | awk '{print $NF}')'"
    },
    "speed_limit": {
      "upload_speed": 1048576,
      "download_speed": 1048576
    },
    "ip_limit": 3
  },
  "operation": "Add"
}' 127.0.0.1:10000 trojan.api.TrojanServerService/SetUsers
```

### 查看服务描述

```bash
# 列出所有服务
grpcurl -plaintext 127.0.0.1:10000 list

# 查看服务方法
grpcurl -plaintext 127.0.0.1:10000 list trojan.api.TrojanServerService

# 查看消息结构
grpcurl -plaintext 127.0.0.1:10000 describe trojan.api.UserStatus
```

## 使用内置命令行工具

Trojan-Go-Next 编译时包含 `api` 标签后，自带命令行管理工具：

```bash
# 列出所有用户
trojan-go-next -api -addr 127.0.0.1:10000 -list

# 添加用户
trojan-go-next -api -addr 127.0.0.1:10000 -add -password "newpassword"

# 删除用户
trojan-go-next -api -addr 127.0.0.1:10000 -delete -password "oldpassword"

# 修改用户限速（1 MB/s 上传 + 下载，最多 3 个 IP）
trojan-go-next -api -addr 127.0.0.1:10000 \
  -modify \
  -password "userpass" \
  -upload-speed-limit 1048576 \
  -download-speed-limit 1048576 \
  -ip-limit 3
```

## 开发自定义 API 客户端

基于 api.proto 生成客户端代码：

```bash
# 安装 protoc 和 Go 插件
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 生成 Go 代码
protoc --go_out=. --go-grpc_out=. \
  api/service/api.proto
```

Go 客户端示例：

```go
package main

import (
    "context"
    "crypto/tls"
    "fmt"
    "log"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"
)

func main() {
    // 建立 gRPC 连接
    creds := credentials.NewTLS(&tls.Config{
        InsecureSkipVerify: true,
    })
    conn, err := grpc.Dial("127.0.0.1:10000",
        grpc.WithTransportCredentials(creds),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    // 列出所有用户
    client := api.NewTrojanServerServiceClient(conn)
    stream, err := client.ListUsers(ctx, &api.ListUsersRequest{})
    if err != nil {
        log.Fatal(err)
    }

    for {
        resp, err := stream.Recv()
        if err != nil {
            break
        }
        status := resp.GetStatus()
        fmt.Printf("User: %s  Upload: %d  Download: %d  Speed↑: %d  Speed↓: %d  IPs: %d/%d\n",
            status.GetUser().GetHash(),
            status.GetTrafficTotal().GetUploadTraffic(),
            status.GetTrafficTotal().GetDownloadTraffic(),
            status.GetSpeedCurrent().GetUploadSpeed(),
            status.GetSpeedCurrent().GetDownloadSpeed(),
            status.GetIpCurrent(),
            status.GetIpLimit(),
        )
    }
}
```
