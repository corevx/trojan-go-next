FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make curl

WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
RUN GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build \
    -tags "full" -trimpath \
    -ldflags="-s -w -buildid= -X github.com/p4gefau1t/trojan-go/constant.Version=${VERSION} -X github.com/p4gefau1t/trojan-go/constant.Commit=${COMMIT}" \
    -o build/trojan-go

RUN cd build && \
    curl -fsSL -o geosite.dat "https://cdn.jsdelivr.net/gh/v2fly/domain-list-community@release/dlc.dat" && \
    curl -fsSL -o geoip.dat "https://cdn.jsdelivr.net/gh/v2fly/geoip@release/geoip.dat" && \
    curl -fsSL -o geoip-only-cn-private.dat "https://cdn.jsdelivr.net/gh/v2fly/geoip@release/geoip-only-cn-private.dat"

FROM alpine:3.20

RUN apk add --no-cache tzdata ca-certificates

WORKDIR /etc/trojan-go
COPY --from=builder /src/build/trojan-go /usr/local/bin/trojan-go
COPY --from=builder /src/build/*.dat /etc/trojan-go/

EXPOSE 443 8443

ENTRYPOINT ["/usr/local/bin/trojan-go", "-config"]
CMD ["/etc/trojan-go/config.json"]
