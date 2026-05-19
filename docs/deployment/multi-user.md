---
title: 多用户管理
---

# 多用户管理

Trojan-Go-Next 支持多种用户管理方式，从简单的配置文件到完整的 API 管理。

## 方式一：配置文件多密码

最简单的方式，在服务端 `password` 数组中配置多个密码：

```json
{
    "run_type": "server",
    "password": [
        "password-for-alice",
        "password-for-bob",
        "password-for-charlie"
    ]
}
```

每个密码对应一个用户。客户端使用对应密码即可连接。

**适用场景：** 用户数量少（10 人以内），不需要流量统计和配额管理。

## 方式二：MySQL 数据库

将用户数据存储在 MySQL 中，支持流量统计和配额管理。

### 配置

```json
{
    "mysql": {
        "enabled": true,
        "server_addr": "localhost",
        "server_port": 3306,
        "database": "trojan_go",
        "username": "trojan",
        "password": "db-password",
        "check_rate": 60
    }
}
```

`check_rate` 是从数据库同步用户数据的间隔（秒）。

### 创建数据库表

```sql
CREATE TABLE users (
    id INT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL,
    password CHAR(56) NOT NULL,
    quota BIGINT NOT NULL DEFAULT 0,
    download BIGINT UNSIGNED NOT NULL DEFAULT 0,
    upload BIGINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    INDEX (password)
);
```

### 添加用户

密码需要用 SHA224 哈希后存储：

```shell
echo -n "user-password" | openssl dgst -sha224 | awk '{print $NF}'
```

```sql
INSERT INTO users (username, password, quota)
VALUES ('alice', 'sha224_hash_here', 10737418240);
-- quota 单位为字节，10737418240 = 10GB
```

### 管理用户

- **添加用户**：插入新记录，Trojan-Go-Next 会在下一个检查周期自动加载
- **删除用户**：删除记录即可
- **设置配额**：修改 `quota` 字段（单位：字节）
- **重置流量**：将 `download` 和 `upload` 设为 0
- **自动禁用**：当 `download + upload > quota` 时自动拒绝连接

**适用场景：** 中等规模（10-100 人），需要流量统计和配额管理。

## 方式三：REST API (v0.11.0)

通过 HTTP REST API 动态管理用户，适合集成到管理面板。

### 配置

```json
{
    "rest": {
        "enabled": true,
        "rest_port": 8443,
        "api_key": "your-secret-api-key",
        "cors": ["https://panel.example.com"]
    }
}
```

### 管理用户

```shell
# 列出所有用户
curl -H "X-API-Key: your-secret-api-key" https://localhost:8443/api/v1/users

# 添加用户
curl -X POST -H "X-API-Key: your-secret-api-key" \
    -H "Content-Type: application/json" \
    -d '{"password": "user-password"}' \
    https://localhost:8443/api/v1/users

# 删除用户
curl -X DELETE -H "X-API-Key: your-secret-api-key" \
    https://localhost:8443/api/v1/users/{hash}

# 查看流量
curl -H "X-API-Key: your-secret-api-key" \
    https://localhost:8443/api/v1/traffic/{hash}
```

详细 API 文档参见 [REST API](/management/rest-api)。

**适用场景：** 需要自动化管理、集成到面板或脚本中。

## 方式四：gRPC API

通过命令行工具远程管理用户。

### 配置

```json
{
    "api": {
        "enabled": true,
        "api_addr": "127.0.0.1",
        "api_port": 10000
    }
}
```

### 使用

```shell
# 列出所有用户
./trojan-go-next -api list -api-addr 127.0.0.1:10000

# 添加用户
./trojan-go-next -api set -api-addr 127.0.0.1:10000 \
    -add-profile -target-password "user-password"

# 删除用户
./trojan-go-next -api set -api-addr 127.0.0.1:10000 \
    -delete-profile -target-password "user-password"

# 设置限速（上传/下载，单位 KB/s）
./trojan-go-next -api set -api-addr 127.0.0.1:10000 \
    -target-password "user-password" \
    -upload-speed-limit 1024 \
    -download-speed-limit 1024
```

详细使用说明参见 [gRPC API](/management/grpc-api)。

## 方式对比

| 方式 | 用户规模 | 流量统计 | 配额管理 | 动态增删 | 复杂度 |
|------|---------|---------|---------|---------|--------|
| 配置文件 | < 10 | ❌ | ❌ | 需重启 | 低 |
| MySQL | 10-100 | ✅ | ✅ | 自动同步 | 中 |
| REST API | 不限 | ✅ | ✅ | 实时 | 中 |
| gRPC API | 不限 | ✅ | ✅ | 实时 | 中 |
