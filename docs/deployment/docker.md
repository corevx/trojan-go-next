---
title: Docker 容器部署
---

# Docker 容器部署

Trojan-Go 提供官方 Docker 镜像，支持一键部署。

## 快速启动

```shell
docker run --name trojan-go -d \
    -v /etc/trojan-go/:/etc/trojan-go \
    --network host \
    ghcr.io/corevx/trojan-go-next
```

默认读取 `/etc/trojan-go/config.json`。

## 指定配置文件

```shell
docker run --name trojan-go -d \
    -v /path/to/host/config:/path/in/container \
    --network host \
    ghcr.io/corevx/trojan-go-next \
    /path/in/container/config.json
```

## 使用 docker-compose（推荐）

创建 `docker-compose.yml`：

```yaml
services:
  trojan-go:
    image: ghcr.io/corevx/trojan-go-next
    container_name: trojan-go
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./config.json:/etc/trojan-go/config.json
      - ./certs:/etc/trojan-go/certs:ro
    environment:
      - TZ=Asia/Shanghai
```

启动：

```shell
docker compose up -d
```

## 常用操作

```shell
# 查看日志
docker logs -f trojan-go

# 重启
docker restart trojan-go

# 停止
docker compose down

# 更新镜像
docker compose pull
docker compose up -d
```

## 挂载证书

将证书文件挂载到容器内：

```yaml
volumes:
  - ./config.json:/etc/trojan-go/config.json
  - /etc/letsencrypt/live/example.com/fullchain.pem:/etc/trojan-go/certs/cert.pem:ro
  - /etc/letsencrypt/live/example.com/privkey.pem:/etc/trojan-go/certs/key.pem:ro
```

配置文件中对应路径：

```json
{
    "ssl": {
        "cert": "/etc/trojan-go/certs/cert.pem",
        "key": "/etc/trojan-go/certs/key.pem"
    }
}
```

## 注意事项

- **必须使用 `--network host`**：Trojan-Go 需要监听 443 端口，使用 host 网络模式可以避免 NAT 和端口映射问题
- **镜像内置 GeoIP/GeoSite 数据**：无需额外挂载
- **权限问题**：如果绑定 443 端口需要 root 权限，确保 Docker 有 `CAP_NET_BIND_SERVICE` 能力
- **时区**：通过 `TZ` 环境变量设置时区，影响日志时间戳
