---
title: 常见问题
---

# 常见问题

## 连接相关

### 客户端无法连接服务端

**症状：** 客户端启动后无法上网，或日志中显示连接超时 / 拒绝。

**排查步骤：**

1. **检查服务端是否运行**
   ```shell
   # 在服务端执行
   ps aux | grep trojan-go
   ss -tlnp | grep 443
   ```

2. **检查防火墙**
   ```shell
   # 开放 443 端口
   sudo ufw allow 443/tcp     # UFW
   sudo firewall-cmd --add-port=443/tcp --permanent  # firewalld
   ```

3. **检查云服务商安全组** — 如果使用阿里云、腾讯云等 VPS，需要在控制台的安全组中放行对应端口。

4. **检查密码是否一致** — 确认客户端和服务端的 `password` 字段完全相同。

5. **检查域名解析** — 确认域名 A 记录指向正确的服务器 IP：
   ```shell
   nslookup your-domain.com
   ```

### 服务端启动报错 "HTTP server is not working"

**原因：** Trojan-Go 会检测 `remote_addr:remote_port` 上的 HTTP 服务是否正常工作。如果 HTTP 服务未启动，Trojan-Go 将拒绝启动。

**解决方法：**
- 确保服务器本地 80 端口（或你配置的 `remote_port`）上运行了 HTTP 服务（如 Nginx）
- 可以简单安装 Nginx 并启动：`sudo apt install nginx && sudo systemctl start nginx`

### 连接速度慢

可能原因：
1. **未启用多路复用** — 在客户端配置中启用 mux：
   ```json
   "mux": {
       "enabled": true
   }
   ```
2. **服务器带宽不足** — 检查 VPS 的网络带宽和流量限制
3. **MTU 问题** — 尝试调整系统 MTU：`sudo ip link set eth0 mtu 1400`

## 证书相关

### 证书错误 "certificate verify failed"

**原因：** 客户端无法验证服务端的 TLS 证书。

**排查步骤：**
1. 确认证书没有过期：
   ```shell
   openssl x509 -in cert.crt -noout -dates
   ```
2. 确认域名和证书匹配 — 证书的 Common Name 或 SAN 必须与访问域名一致
3. 如果使用自签证书，客户端需要配置：
   ```json
   "ssl": {
       "verify": false
   }
   ```
   ::: warning
   生产环境不建议禁用证书验证。
   :::

### 如何续期 Let's Encrypt 证书

```shell
sudo certbot renew
sudo systemctl restart trojan-go
```

推荐设置自动续期（certbot 默认会安装 cron 任务）：

```shell
# 测试续期是否正常
sudo certbot renew --dry-run
```

### 如何使用自签证书

生成自签证书：

```shell
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.crt \
    -days 365 -nodes -subj "/CN=example.com"
```

服务端使用自签证书后，客户端需要配置 `verify: false` 或将自签 CA 导入系统信任列表。

## 配置相关

### JSON 和 YAML 格式有什么区别

Trojan-Go 同时支持 JSON 和 YAML 两种配置格式，功能完全相同。YAML 格式更易读写：

```yaml
# YAML 格式
run-type: client
local-addr: 127.0.0.1
local-port: 1080
remote-addr: example.com
remote-port: 443
password:
  - your_password
```

等价于：

```json
{
    "run_type": "client",
    "local_addr": "127.0.0.1",
    "local_port": 1080,
    "remote_addr": "example.com",
    "remote_port": 443,
    "password": ["your_password"]
}
```

### 如何配置多个用户

在服务端 `password` 数组中添加多个密码，每个密码对应一个用户：

```json
{
    "run_type": "server",
    "password": [
        "password_for_user_1",
        "password_for_user_2",
        "password_for_user_3"
    ]
}
```

### 配置文件支持哪些环境变量

- `TROJAN_GO_LOCATION_ASSET` — GeoIP/GeoSite 数据文件搜索路径（默认为二进制文件所在目录）

## 兼容性

### Trojan-Go 服务端兼容哪些客户端

所有支持标准 Trojan 协议的客户端都可以连接 Trojan-Go 服务端，包括：

- [v2rayN](https://github.com/2dust/v2rayN)
- [Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev)
- [NekoBox for Android](https://github.com/MatsuriDayo/NekoBoxForAndroid)
- [ShadowRocket](https://apps.apple.com/app/shadowrocket/id932747118)
- [sing-box](https://github.com/SagerNet/sing-box)

::: warning
以上客户端仅支持标准 Trojan 协议。Trojan-Go 扩展特性（WebSocket 传输、多路复用、AEAD 二次加密）需要双方都使用 `trojan-go` 二进制。
:::

### Trojan-Go 客户端能连接原版 Trojan 服务端吗

可以。只要不启用 Trojan-Go 的扩展特性（WebSocket、mux、shadowsocks 等），客户端完全兼容原版 Trojan 服务端。

## Docker 相关

### Docker 部署时如何指定配置文件

```shell
docker run --name trojan-go -d \
    -v /etc/trojan-go/:/etc/trojan-go \
    --network host \
    ghcr.io/corevx/trojan-go-next
```

默认配置文件路径为 `/etc/trojan-go/config.json`。也可以指定其他路径：

```shell
docker run --name trojan-go -d \
    -v /my/config/:/config/ \
    --network host \
    ghcr.io/corevx/trojan-go-next \
    /config/my-server.json
```

### Docker 容器内如何使用 GeoIP 数据

Docker 镜像已内置 GeoIP / GeoSite 数据文件。如果你需要使用自定义数据文件，将其挂载到容器内即可：

```shell
docker run --name trojan-go -d \
    -v /etc/trojan-go/:/etc/trojan-go \
    --network host \
    ghcr.io/corevx/trojan-go-next
```

## 安全相关

### 如何提高安全性

1. **使用强密码** — `password` 应使用随机生成的长字符串
2. **使用 CA 签名的证书** — 避免使用自签证书
3. **启用 fallback_port** — 对非 TLS 流量返回正常网页
4. **配置本地 HTTP 服务** — 让服务器在浏览器访问时显示正常网页
5. **开启 AEAD 二次加密** — 防止 CDN 审查流量
6. **定期更新** — 保持使用最新版本

### 服务端被封锁了怎么办

1. 确认是 IP 被封还是端口被封锁
2. 更换服务器 IP 或端口
3. 使用 WebSocket + CDN 中转（推荐 Cloudflare），这样真实 IP 不会暴露
4. 参见 [WebSocket CDN 中转](/features/websocket)

## 其他

### 如何查看版本号

```shell
./trojan-go -version
```

### 如何查看运行日志

```shell
# systemd 服务
journalctl -u trojan-go -f

# Docker
docker logs -f trojan-go
```

### GeoIP/GeoSite 数据文件在哪里下载

Trojan-Go 使用 V2Fly 的数据文件：
- [GeoIP](https://github.com/v2fly/geoip)
- [GeoSite (domain-list-community)](https://github.com/v2fly/domain-list-community)

如果使用路由分流功能，需要将 `.dat` 文件放在 Trojan-Go 二进制同目录下，或设置 `TROJAN_GO_LOCATION_ASSET` 环境变量。
