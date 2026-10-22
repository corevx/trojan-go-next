package scenario

// ShadowTLS v3 服务端互操作测试（规格 demos/07 §8 矩阵）：
// 客户端 = 真实 mihomo / sing-box 二进制（trojan-go-interop 测试床），
// 服务端 = 本仓 tunnel/shadowtls。测试床缺失（CI）时自动 Skip。
//
// 运行：SHADOWSOCKS_SF_CAPACITY="-1" go test -race -v -run TestInteropShadowTLS ./test/scenario/

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	netproxy "golang.org/x/net/proxy"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/proxy"
)

const (
	interopSocksUser     = "stls-u1"
	interopSocksPassword = "shadowtls-pass"
	interopTrojanPass    = "trojan-pass"
	interopRealSiteSNI   = "www.example.com"
)

func interopDir() string {
	if d := os.Getenv("TROJAN_GO_INTEROP_DIR"); d != "" {
		return d
	}
	return "/Users/toimc/Projects/trojan-go-interop"
}

func interopMihomoBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(interopDir(), "bin", "mihomo-darwin-arm64-v1.19.33")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("mihomo interop binary not found at %s (testbed absent, e.g. CI)", bin)
	}
	return bin
}

func interopSingBoxBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(interopDir(), "bin", "sing-box")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("sing-box interop binary not found at %s (testbed absent, e.g. CI)", bin)
	}
	return bin
}

// stlsRealSite 启动被伪装的真站：自签 CN=www.example.com 的 TLS 站点，
// maxVersion 控制版本（矩阵用例 5/6/7）
func stlsRealSite(t *testing.T, maxVersion uint16) (addr string, cert *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: interopRealSiteSNI},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{interopRealSiteSNI},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	tlsCert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cfg := &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12, MaxVersion: maxVersion}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				tlsConn := tls.Server(conn, cfg)
				if err := tlsConn.Handshake(); err != nil {
					return
				}
				buf := make([]byte, 8192)
				_ = tlsConn.SetReadDeadline(time.Now().Add(15 * time.Second))
				_, _ = tlsConn.Read(buf)
				_, _ = tlsConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 18\r\n\r\nreal-site-payload\r\n"))
			}(conn)
		}
	}()
	return ln.Addr().String(), cert
}

// syncedLogBuf 并发安全的日志缓冲：服务端各 goroutine 持续写入，测试侧读取
type syncedLogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncedLogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncedLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// stlsServer 启动 trojan-go-next 服务端（transport→shadowtls→trojan），
// 返回 shadowtls 监听地址与日志缓冲
func stlsServer(t *testing.T, realSite string, strict bool) (addr string, logBuf *syncedLogBuf) {
	t.Helper()
	port := common.PickPort("tcp", "127.0.0.1")
	host, portStr, _ := net.SplitHostPort(realSite)
	destPort := 0
	fmt.Sscanf(portStr, "%d", &destPort)
	cfgJSON := fmt.Sprintf(`{
  "run_type": "server",
  "local_addr": "127.0.0.1",
  "local_port": %d,
  "password": ["%s"],
  "disable_http_check": true,
  "log_level": 1,
  "shadowtls": {
    "enabled": true,
    "version": 3,
    "users": [{"name": "%s", "password": "%s"}],
    "handshake": {"server": "%s", "server_port": %d},
    "strict": %t
  }
}`, port, interopTrojanPass, interopSocksUser, interopSocksPassword, host, destPort, strict)

	buf := &syncedLogBuf{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	server, err := proxy.NewProxyFromConfigData([]byte(cfgJSON), true)
	if err != nil {
		t.Fatal("failed to create shadowtls server:", err)
	}
	go server.Run()
	t.Cleanup(func() { server.Close() })
	return fmt.Sprintf("127.0.0.1:%d", port), buf
}

func stlsHTTPTarget(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "echo-path-%s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// runInteropProc 启动客户端二进制并等待其本地入口端口就绪
func runInteropProc(t *testing.T, name string, args []string, readyAddr string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", readyAddr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not become ready at %s", name, readyAddr)
}

func stlsMihomoConfig(t *testing.T, serverAddr string, socksPort, version int) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	cfgPath = filepath.Join(dir, "mihomo.yaml")
	cfg := fmt.Sprintf(`mixed-port: %d
mode: rule
log-level: info
proxies:
  - name: stls3-trojan
    type: trojan
    server: 127.0.0.1
    port: %s
    password: %s
    udp: true
    sni: %s
    skip-cert-verify: true
    client-fingerprint: chrome
    shadow-tls-opts:
      version: %d
      password: %s
rules:
  - MATCH,stls3-trojan
`, socksPort, serverPortOf(serverAddr), interopTrojanPass, interopRealSiteSNI, version, interopSocksPassword)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfgPath
}

func serverPortOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}

func stlsFetch(t *testing.T, socksPort int, url string) (int, string, error) {
	t.Helper()
	dialer, err := netproxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, netproxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		DialContext: dialer.(netproxy.ContextDialer).DialContext,
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, "", err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(body), nil
}

func stlsFetchOK(t *testing.T, socksPort int, url, wantPrefix string) {
	t.Helper()
	status, body, err := stlsFetch(t, socksPort, url)
	if err != nil {
		t.Fatalf("proxied fetch failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if !strings.HasPrefix(body, wantPrefix) {
		t.Fatalf("body %q does not come from %q", body, wantPrefix)
	}
}

// 矩阵用例 1 + 5：mihomo v3 客户端正常代理（strict × TLS1.3 真站）
func TestInteropShadowTLSMihomoV3(t *testing.T) {
	mihomo := interopMihomoBin(t)
	realSite, _ := stlsRealSite(t, tls.VersionTLS13)
	serverAddr, logBuf := stlsServer(t, realSite, true)
	target := stlsHTTPTarget(t)

	socksPort := common.PickPort("tcp", "127.0.0.1")
	dir, cfgPath := stlsMihomoConfig(t, serverAddr, socksPort, 3)
	runInteropProc(t, mihomo, []string{"-d", dir, "-f", cfgPath}, fmt.Sprintf("127.0.0.1:%d", socksPort))

	stlsFetchOK(t, socksPort, target+"/mihomo-case1", "echo-path-/mihomo-case1")

	// 服务端日志应出现验签通过的用户名（对应 mihomo UserFromConn 语义）
	if !strings.Contains(logBuf.String(), interopSocksUser) {
		t.Fatalf("server log does not mention authenticated user %q; log:\n%s", interopSocksUser, logBuf.String())
	}
}

// 矩阵用例 2 + 5：sing-box v3 客户端正常代理 + 并发 10 连接（链状态独立性）
func TestInteropShadowTLSSingBoxV3(t *testing.T) {
	singBox := interopSingBoxBin(t)
	realSite, _ := stlsRealSite(t, tls.VersionTLS13)
	serverAddr, _ := stlsServer(t, realSite, true)
	target := stlsHTTPTarget(t)

	socksPort := common.PickPort("tcp", "127.0.0.1")
	cfgPath := filepath.Join(t.TempDir(), "sing-box.json")
	cfg := fmt.Sprintf(`{
  "log": {"level": "info"},
  "inbounds": [{
    "type": "mixed",
    "tag": "mixed-in",
    "listen": "127.0.0.1",
    "listen_port": %d
  }],
  "outbounds": [
    {
      "type": "shadowtls",
      "tag": "st-out",
      "server": "127.0.0.1",
      "server_port": %s,
      "version": 3,
      "password": "%s",
      "tls": {
        "enabled": true,
        "server_name": "%s",
        "insecure": true,
        "utls": {"enabled": true, "fingerprint": "chrome"}
      }
    },
    {
      "type": "trojan",
      "tag": "proxy-out",
      "server": "127.0.0.1",
      "server_port": %s,
      "password": "%s",
      "detour": "st-out"
    }
  ],
  "route": {"final": "proxy-out"}
}`, socksPort, serverPortOf(serverAddr), interopSocksPassword, interopRealSiteSNI, serverPortOf(serverAddr), interopTrojanPass)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	runInteropProc(t, singBox, []string{"run", "-c", cfgPath}, fmt.Sprintf("127.0.0.1:%d", socksPort))

	stlsFetchOK(t, socksPort, target+"/singbox-case2", "echo-path-/singbox-case2")

	const concurrency = 10
	wg := sync.WaitGroup{}
	wg.Add(concurrency)
	errs := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			status, body, err := stlsFetch(t, socksPort, fmt.Sprintf("%s/concurrent-%d", target, i))
			if err != nil {
				errs <- err
				return
			}
			if status != 200 || body != fmt.Sprintf("echo-path-/concurrent-%d", i) {
				errs <- fmt.Errorf("concurrent %d: status %d body %q", i, status, body)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal("concurrent fetch failed:", err)
	}
}

// 矩阵用例 3 + 4：主动探测——直连服务端口只看到真站证书与真站内容，
// 无任何 trojan/shadowtls 特征；探测后服务端仍健康
func TestInteropShadowTLSProbeResistance(t *testing.T) {
	realSite, realCert := stlsRealSite(t, tls.VersionTLS13)
	serverAddr, _ := stlsServer(t, realSite, true)

	// 普通 TLS 探测（Go stdlib 客户端，session_id 无签名 → 服务端回退中继）
	conn, err := tls.Dial("tcp", serverAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         interopRealSiteSNI,
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("probe TLS handshake failed (prober must see a real-site handshake): %v", err)
	}
	peerCert := conn.ConnectionState().PeerCertificates[0]
	if peerCert.Subject.CommonName != realCert.Subject.CommonName {
		t.Fatalf("probe saw cert CN=%q, expected real-site CN=%q", peerCert.Subject.CommonName, realCert.Subject.CommonName)
	}
	req := "GET /probe HTTP/1.1\r\nHost: " + interopRealSiteSNI + "\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	respBytes, _ := io.ReadAll(conn)
	conn.Close()
	if !strings.Contains(string(respBytes), "real-site-payload") {
		t.Fatalf("probe must receive real-site content, got %q", respBytes)
	}

	// 探测后服务端必须仍然可用：mihomo 正常代理
	mihomo := interopMihomoBin(t)
	target := stlsHTTPTarget(t)
	socksPort := common.PickPort("tcp", "127.0.0.1")
	dir, cfgPath := stlsMihomoConfig(t, serverAddr, socksPort, 3)
	runInteropProc(t, mihomo, []string{"-d", dir, "-f", cfgPath}, fmt.Sprintf("127.0.0.1:%d", socksPort))
	stlsFetchOK(t, socksPort, target+"/after-probe", "echo-path-/after-probe")
}

// 矩阵用例 6：strict × 非 TLS1.3 真站 → 客户端拿不到代理服务，服务端无崩溃
func TestInteropShadowTLSStrictNonTLS13(t *testing.T) {
	mihomo := interopMihomoBin(t)
	realSite, _ := stlsRealSite(t, tls.VersionTLS12)
	serverAddr, _ := stlsServer(t, realSite, true)
	target := stlsHTTPTarget(t)

	socksPort := common.PickPort("tcp", "127.0.0.1")
	dir, cfgPath := stlsMihomoConfig(t, serverAddr, socksPort, 3)
	runInteropProc(t, mihomo, []string{"-d", dir, "-f", cfgPath}, fmt.Sprintf("127.0.0.1:%d", socksPort))

	if _, _, err := stlsFetch(t, socksPort, target+"/strict-v12"); err == nil {
		t.Fatal("strict server with TLS1.2-only real site must not serve proxy traffic")
	}
}

// 矩阵用例 7：非 strict × 非 TLS1.3 真站 → 仍可正常代理（lossy 模式）
func TestInteropShadowTLSLossyNonTLS13(t *testing.T) {
	mihomo := interopMihomoBin(t)
	realSite, _ := stlsRealSite(t, tls.VersionTLS12)
	serverAddr, _ := stlsServer(t, realSite, false)
	target := stlsHTTPTarget(t)

	socksPort := common.PickPort("tcp", "127.0.0.1")
	dir, cfgPath := stlsMihomoConfig(t, serverAddr, socksPort, 3)
	runInteropProc(t, mihomo, []string{"-d", dir, "-f", cfgPath}, fmt.Sprintf("127.0.0.1:%d", socksPort))

	stlsFetchOK(t, socksPort, target+"/lossy-v12", "echo-path-/lossy-v12")
}

// 矩阵用例 9：v2 客户端 → v3 服务端——无代理成功、服务端不崩溃，
// 随后 v3 客户端仍可正常使用（回退中继不污染服务端状态）
func TestInteropShadowTLSV2ClientToV3Server(t *testing.T) {
	mihomo := interopMihomoBin(t)
	realSite, _ := stlsRealSite(t, tls.VersionTLS13)
	serverAddr, _ := stlsServer(t, realSite, true)
	target := stlsHTTPTarget(t)

	v2Socks := common.PickPort("tcp", "127.0.0.1")
	dir2, cfg2 := stlsMihomoConfig(t, serverAddr, v2Socks, 2)
	runInteropProc(t, mihomo, []string{"-d", dir2, "-f", cfg2}, fmt.Sprintf("127.0.0.1:%d", v2Socks))

	if _, _, err := stlsFetch(t, v2Socks, target+"/v2-attempt"); err == nil {
		t.Fatal("v2 client against v3 server must not succeed")
	}

	v3Socks := common.PickPort("tcp", "127.0.0.1")
	dir3, cfg3 := stlsMihomoConfig(t, serverAddr, v3Socks, 3)
	runInteropProc(t, mihomo, []string{"-d", dir3, "-f", cfg3}, fmt.Sprintf("127.0.0.1:%d", v3Socks))
	stlsFetchOK(t, v3Socks, target+"/v3-after-v2", "echo-path-/v3-after-v2")
}
