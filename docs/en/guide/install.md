---
title: Installation
---

# Installation

This guide walks you through deploying Trojan-Go-Next from scratch.

## Prerequisites

| Item | Description |
|------|-------------|
| A server | VPS located outside the firewall, running Ubuntu / Debian / CentOS recommended |
| A domain name | Free domains (e.g. .tk) or paid domains both work |
| TLS certificate | Free via Let's Encrypt, or self-signed |
| DNS record | Point your domain's A record to your server IP |

## Download

### Pre-built Binaries (Recommended)

Download from the [Release page](https://github.com/corevx/trojan-go-next-next/releases):

```shell
# Linux amd64 example
wget https://github.com/corevx/trojan-go-next-next/releases/latest/download/trojan-go-next-linux-amd64.zip
unzip trojan-go-next-linux-amd64.zip
chmod +x trojan-go-next
```

Supported platforms: Linux (amd64, arm, arm64, mips), macOS (Intel, Apple Silicon), Windows, FreeBSD.

### Docker

```shell
docker pull ghcr.io/corevx/trojan-go-next-next

docker run --name trojan-go-next -d \
    -v /etc/trojan-go-next/:/etc/trojan-go-next \
    --network host \
    ghcr.io/corevx/trojan-go-next-next
```

### Build from Source

Requires Go >= 1.22:

```shell
git clone https://github.com/corevx/trojan-go-next-next.git
cd trojan-go-next
make
```

## TLS Certificate

Trojan-Go-Next requires a TLS certificate. Use Let's Encrypt for a free CA-signed cert:

```shell
sudo apt install certbot
sudo certbot certonly --standalone -d example.com
```

Certificate files will be at `/etc/letsencrypt/live/example.com/`:
- `fullchain.pem` → config `cert`
- `privkey.pem` → config `key`

## Quick Start (Easy Mode)

Server:

```shell
sudo ./trojan-go-next -server \
    -remote 127.0.0.1:80 \
    -local 0.0.0.0:443 \
    -key /etc/letsencrypt/live/example.com/privkey.pem \
    -cert /etc/letsencrypt/live/example.com/fullchain.pem \
    -password your_password
```

Client:

```shell
./trojan-go-next -client \
    -remote example.com:443 \
    -local 127.0.0.1:1080 \
    -password your_password
```

After the client starts, `127.0.0.1:1080` is a SOCKS5/HTTP proxy port.

## Configuration File (Recommended)

### Server Config (`server.json`)

```json
{
    "run_type": "server",
    "local_addr": "0.0.0.0",
    "local_port": 443,
    "remote_addr": "127.0.0.1",
    "remote_port": 80,
    "password": ["your_password"],
    "ssl": {
        "cert": "/etc/letsencrypt/live/example.com/fullchain.pem",
        "key": "/etc/letsencrypt/live/example.com/privkey.pem"
    }
}
```

`remote_addr:remote_port` points to a local HTTP service. When non-Trojan traffic arrives (browser access, active probing), Trojan-Go-Next forwards it there, making your server look like a normal HTTPS website.

### Client Config (`client.json`)

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

## Systemd Service

1. Install the binary and service file:

```shell
sudo cp trojan-go-next /usr/bin/trojan-go-next
```

2. Create `/usr/lib/systemd/system/trojan-go-next.service`:

```ini
[Unit]
Description=Trojan-Go-Next
After=network.target nss-lookup.target

[Service]
User=nobody
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=/usr/bin/trojan-go-next -config /etc/trojan-go-next/config.json
Restart=on-failure
RestartSec=10s
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
```

3. Enable and start:

```shell
sudo systemctl daemon-reload
sudo systemctl enable trojan-go-next
sudo systemctl start trojan-go-next
```

## Verify

### Server

Visit `https://example.com` in a browser — you should see your normal web page.

### Client

```shell
curl --socks5 127.0.0.1:1080 https://www.google.com -I
```

If you get `HTTP 200`, the proxy is working.
