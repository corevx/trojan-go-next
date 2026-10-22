package reality

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
	"github.com/p4gefau1t/trojan-go/tunnel/trojan"
)

// singboxBin 按 SINGBOX_BIN 环境变量 → PATH → 本机 interop 目录的顺序
// 定位 sing-box 可执行文件；找不到则跳过（CI 无二进制时互操作测试不阻塞）。
func singboxBin(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if p := os.Getenv("SINGBOX_BIN"); p != "" {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		candidates = append(candidates, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Projects", "trojan-go-interop", "bin", "sing-box"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	t.Skipf("sing-box binary not found (set SINGBOX_BIN to enable), candidates: %v", candidates)
	return ""
}

const (
	interpServerName = "www.example.com"
	interpPassword   = "trojan-pass"
	interpShortID    = "0123456789abcdef"
)

// interpFixture 汇集互操作测试的运行面：x25519 密钥对、回落真站、echo
// 目标、sing-box 进程与其 trojan+REALITY 入站地址。
type interpFixture struct {
	singboxAddr  string
	echoAddr     string
	fallbackAddr string
	privB64      string
	pubB64       string
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(conn, conn)
				_ = conn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// startFallbackHTTPS 提供 REALITY 的回落真站：本地 TLS 1.3 服务（Go 默认
// 支持 x25519），自签证书。认证成功路径不会校验它的证书；失败路径会因
// 无法通过标准校验而报错——两种行为都是测试断言的一部分。
func startFallbackHTTPS(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func startSingbox(t *testing.T, f *interpFixture) {
	t.Helper()
	bin := singboxBin(t)
	dir := t.TempDir()

	cfg := map[string]interface{}{
		"log": map[string]interface{}{"level": "warn"},
		"inbounds": []interface{}{
			map[string]interface{}{
				"type":        "trojan",
				"tag":         "tr-in",
				"listen":      "127.0.0.1",
				"listen_port": mustPort(t, f.singboxAddr),
				"users": []interface{}{
					map[string]interface{}{"name": "u1", "password": interpPassword},
				},
				"tls": map[string]interface{}{
					"enabled":     true,
					"server_name": interpServerName,
					"reality": map[string]interface{}{
						"enabled": true,
						"handshake": map[string]interface{}{
							"server":      "127.0.0.1",
							"server_port": mustPort(t, f.fallbackAddr),
						},
						"private_key": f.privB64,
						"short_id":    []string{interpShortID},
					},
				},
			},
		},
		"outbounds": []interface{}{
			map[string]interface{}{"type": "direct", "tag": "direct-out"},
		},
		"route": map[string]interface{}{"final": "direct-out"},
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal sing-box config: %v", err)
	}
	cfgPath := filepath.Join(dir, "sing-box.json")
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatalf("write sing-box config: %v", err)
	}

	cmd := exec.Command(bin, "run", "-c", cfgPath)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sing-box: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// 就绪探测：入站端口可建立 TCP 连接，或进程退出（配置错则打印日志）。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil {
			t.Fatalf("sing-box exited early:\n%s", logs.String())
		}
		conn, err := net.DialTimeout("tcp", f.singboxAddr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("sing-box not ready in 10s:\n%s", logs.String())
}

func mustPort(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return port
}

// buildStack 手工组装 transport→reality→trojan 客户端栈（与 proxy CLIENT
// 模式的 GenerateClientTree(Reality) 等价），返回可直接 DialConn 的顶层。
func buildStack(t *testing.T, serverAddr, pubB64, shortID string) tunnel.Client {
	t.Helper()
	ctx := context.Background()
	ctx = config.WithConfig(ctx, transport.Name, &transport.Config{
		RemoteHost: "127.0.0.1",
		RemotePort: mustPort(t, serverAddr),
	})
	ctx = config.WithConfig(ctx, Name, &Config{
		RemoteHost: "127.0.0.1",
		RemotePort: mustPort(t, serverAddr),
		Reality: RealityConfig{
			Enabled:     true,
			PublicKey:   pubB64,
			ShortID:     shortID,
			ServerName:  interpServerName,
			Fingerprint: "chrome",
		},
	})
	ctx = config.WithConfig(ctx, freedom.Name, &freedom.Config{})
	ctx = config.WithConfig(ctx, trojan.Name, &trojan.Config{})
	ctx = config.WithConfig(ctx, memory.Name, &memory.Config{
		Passwords: []string{interpPassword},
	})

	transportClient, err := transport.NewClient(ctx, nil)
	if err != nil {
		t.Fatalf("transport.NewClient: %v", err)
	}
	realityClient, err := NewClient(ctx, transportClient)
	if err != nil {
		t.Fatalf("reality.NewClient: %v", err)
	}
	trojanClient, err := trojan.NewClient(ctx, realityClient)
	if err != nil {
		t.Fatalf("trojan.NewClient: %v", err)
	}
	return trojanClient
}

// roundtrip 经栈发起一条到 echo 的连接并做一次写读往返。
func roundtrip(t *testing.T, stack tunnel.Client, echoAddr string) error {
	t.Helper()
	addr := tunnel.NewAddressFromHostPort("tcp", "127.0.0.1", mustPort(t, echoAddr))
	conn, err := stack.DialConn(addr, nil)
	if err != nil {
		return fmt.Errorf("DialConn: %w", err)
	}
	defer conn.Close()
	payload := []byte("hello trojan-go reality over sing-box")
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return fmt.Errorf("SetDeadline: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("Write: %w", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		return fmt.Errorf("ReadFull: %w", err)
	}
	if !bytes.Equal(got, payload) {
		return fmt.Errorf("roundtrip mismatch: got %q want %q", got, payload)
	}
	return nil
}

// TestInteropSingboxReality 端到端互操作验收：
//  1. 正确凭证：我方 transport→reality→trojan 栈 ↔ sing-box v1.14.3 的
//     trojan+REALITY 入站，经它代理访问 echo，写读往返成功；
//  2. 错误 short-id / 错误 public-key：服务端判定未认证、透明回落真站，
//     客户端必失败（拿到真站证书，HMAC 回执不成立）。
func TestInteropSingboxReality(t *testing.T) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate x25519: %v", err)
	}
	pubB64 := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	privB64 := base64.RawURLEncoding.EncodeToString(priv.Bytes())

	echoAddr := startEchoServer(t)
	fallbackAddr := startFallbackHTTPS(t)

	singboxAddr := fmt.Sprintf("127.0.0.1:%d", common.PickPort("tcp", "127.0.0.1"))
	f := &interpFixture{
		singboxAddr:  singboxAddr,
		echoAddr:     echoAddr,
		fallbackAddr: fallbackAddr,
		privB64:      privB64,
		pubB64:       pubB64,
	}
	startSingbox(t, f)

	t.Run("roundtrip with correct credentials", func(t *testing.T) {
		stack := buildStack(t, singboxAddr, pubB64, interpShortID)
		defer stack.Close()
		if err := roundtrip(t, stack, echoAddr); err != nil {
			t.Fatalf("roundtrip failed: %v", err)
		}
	})

	t.Run("wrong short-id is rejected", func(t *testing.T) {
		stack := buildStack(t, singboxAddr, pubB64, "ffffffffffffffff")
		defer stack.Close()
		if err := roundtrip(t, stack, echoAddr); err == nil {
			t.Fatal("roundtrip with wrong short-id unexpectedly succeeded")
		} else {
			t.Logf("rejected as expected: %v", err)
		}
	})

	t.Run("wrong public-key is rejected", func(t *testing.T) {
		wrongPriv, _ := ecdh.X25519().GenerateKey(rand.Reader)
		wrongPub := base64.RawURLEncoding.EncodeToString(wrongPriv.PublicKey().Bytes())
		stack := buildStack(t, singboxAddr, wrongPub, interpShortID)
		defer stack.Close()
		if err := roundtrip(t, stack, echoAddr); err == nil {
			t.Fatal("roundtrip with wrong public-key unexpectedly succeeded")
		} else {
			t.Logf("rejected as expected: %v", err)
		}
	})
}
