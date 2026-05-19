---
title: "编译与构建"
---

# 编译与构建

## 前置条件

| 工具 | 最低版本 | 说明 |
|------|----------|------|
| Go | 1.25+ | 编译器，推荐通过 [官方安装包](https://go.dev/dl/) 或 snap 获取 |
| Git | 任意 | 版本控制，用于注入 commit hash 和下载依赖 |
| Make | GNU Make | 执行 Makefile 预设命令（可选，可用 `go build` 替代） |

验证环境：

```bash
go version   # go version go1.25.0 darwin/arm64
git --version
make --version
```

## 基本编译

使用 Makefile（推荐）：

```bash
make
```

产出位于 `build/trojan-go`，默认包含全部功能模块（`-tags "full"`），CGO 已禁用。

等价的 Go 命令：

```bash
CGO_ENABLED=0 go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid=" \
  -o build/trojan-go
```

安装到系统路径（含 systemd 服务和 geoip 数据）：

```bash
make install
```

## 构建标签（Build Tags）

Trojan-Go 的大多数模块通过 Go build tags 控制是否编译。`component/` 目录下的每个文件对应一个标签，通过条件导入决定包含哪些 proxy 和 tunnel 包。

| 标签 | 包含内容 | 等价标签组合 |
|------|----------|-------------|
| `full` | 全部功能 | `api client server forward nat other` |
| `mini` | 精简功能集 | `client server forward nat mysql` |
| `client` | SOCKS/HTTP 代理客户端 | `proxy/client` |
| `server` | Trojan 服务端 | `proxy/server` |
| `forward` | 端口转发模式 | `proxy/forward` |
| `nat` | 透明代理模式（仅 Linux） | `proxy/nat` |
| `custom` | 自定义隧道栈 + 全部 tunnel 模块 | `proxy/custom` + 所有 tunnel 包 |
| `api` | gRPC API 服务 | `api/service` + `api/control` + `api/monitor` + `api/rest` |
| `mysql` | MySQL 用户数据源 | `statistic/mysql` |
| `other` | 辅助功能（easy 模式、URL 解析） | `easy` + `url` |

组合示例：

```bash
# 仅客户端，去除符号表以缩小体积
go build -tags "client" -trimpath -ldflags="-s -w -buildid="

# 服务端 + MySQL 数据源
go build -tags "server mysql"

# 服务端 + API + MySQL
go build -tags "server api mysql"

# 精简版（客户端 + 服务端 + 转发 + 透明代理 + MySQL，不含 API）
go build -tags "mini"
```

## 交叉编译

通过 `GOOS` 和 `GOARCH` 环境变量指定目标平台：

```bash
# Windows x86_64
GOOS=windows GOARCH=amd64 go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid=" -o build/trojan-go.exe

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid=" -o build/trojan-go

# Linux ARM (树莓派等)
GOOS=linux GOARCH=arm GOARM=7 go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid=" -o build/trojan-go

# Linux MIPS LE 软浮点（路由器）
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid=" -o build/trojan-go

# Linux MIPS LE 硬浮点
GOOS=linux GOARCH=mipsle GOMIPS=hardfloat go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid=" -o build/trojan-go
```

## 运行测试

测试需要禁用 Shadowsocks Bloom Filter（避免 CI 环境下误报）：

```bash
# 运行全部测试
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./...

# 运行单个包的测试
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./tunnel/trojan/

# 运行单个测试函数
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./tunnel/trojan/ -run TestTrojan
```

集成测试位于 `test/scenario/` 目录。

## Docker 构建

项目提供了多阶段构建的 Dockerfile：

```bash
# 基本构建
docker build -t trojan-go .

# 指定版本信息
docker build \
  --build-arg VERSION=0.10.6 \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  -t trojan-go:0.10.6 .
```

Dockerfile 结构：

1. **构建阶段**（`golang:1.25-alpine`）：编译二进制 + 下载 geoip/geosite 数据
2. **运行阶段**（`alpine:3.20`）：仅包含二进制和 `.dat` 文件，最终镜像约 49MB

运行容器：

```bash
docker run -d --name trojan-go \
  -v /path/to/config.json:/etc/trojan-go/config.json \
  -p 443:443 \
  trojan-go
```

## 代码检查

使用 golangci-lint 进行静态分析，配置文件位于 `.github/linters/.golangci.yml`：

```bash
golangci-lint run --config=.github/linters/.golangci.yml
```

## Release 构建

`make release` 会交叉编译 22 个平台目标并打包为 zip：

```bash
make release
```

目标平台一览：

| 系统 | 架构 |
|------|------|
| darwin | amd64, arm64 |
| linux | 386, amd64, arm, armv5, armv6, armv7, armv8 (arm64) |
| linux | mips-softfloat, mips-hardfloat, mipsle-softfloat, mipsle-hardfloat, mips64, mips64le |
| freebsd | 386, amd64 |
| windows | 386, amd64, arm, armv6, armv7, arm64 |

每个 zip 包含：二进制文件、示例配置、`geoip.dat`、`geosite.dat`。

## 版本注入

版本号和 commit hash 通过 ldflags 在编译时注入到 `constant` 包：

```go
// constant/constant.go
package constant

var (
    Version = "Custom Version"     // 编译时覆盖
    Commit  = "Unknown Git Commit ID"  // 编译时覆盖
)
```

Makefile 中的注入方式：

```makefile
PACKAGE_NAME := github.com/p4gefau1t/trojan-go
VERSION := `git describe --dirty`
COMMIT  := `git rev-parse HEAD`

VAR_SETTING := -X $(PACKAGE_NAME)/constant.Version=$(VERSION) \
               -X $(PACKAGE_NAME)/constant.Commit=$(COMMIT)
```

手动指定版本：

```bash
go build -tags "full" -trimpath \
  -ldflags="-s -w -buildid= \
    -X github.com/p4gefau1t/trojan-go/constant.Version=0.10.6 \
    -X github.com/p4gefau1t/trojan-go/constant.Commit=$(git rev-parse HEAD)"
```

运行时可通过 `-version` 参数查看注入的版本信息。
