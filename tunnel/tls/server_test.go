package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/p4gefau1t/trojan-go/log"
)

// genSelfSignedCertPEM 生成一对自签 ECDSA 证书（内存 PEM，不落盘），
// CN/DNSNames 用传入的 name，供热重载测试观测 Subject 变化。
func genSelfSignedCertPEM(t *testing.T, name string, serial int64) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// newCheckLoopServer 构造只含 checkKeyPairLoop 依赖字段的最小 Server。
func newCheckLoopServer(ctx context.Context) *Server {
	return &Server{ctx: ctx}
}

func (s *Server) currentCommonName() string {
	s.keyPairLock.RLock()
	defer s.keyPairLock.RUnlock()
	if len(s.keyPair) == 0 || s.keyPair[0].Leaf == nil {
		return ""
	}
	return s.keyPair[0].Leaf.Subject.CommonName
}

// TestCheckKeyPairLoopHotReload 热重载正常路径：落盘新证书对后，
// 循环应在有限时间内把它加载进 s.keyPair（Subject CN 变化可观测）。
func TestCheckKeyPairLoopHotReload(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "server.crt")
	keyPath := filepath.Join(t.TempDir(), "server.key")

	certPEM, keyPEM := genSelfSignedCertPEM(t, "reload-first.example.com", 1)
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newCheckLoopServer(ctx)

	keyPair, err := loadKeyPair(keyPath, certPath, "")
	if err != nil {
		t.Fatal(err)
	}
	s.keyPair = []tls.Certificate{*keyPair}

	const checkRate = 50 * time.Millisecond
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		s.checkKeyPairLoop(checkRate, keyPath, certPath, "")
	}()
	defer func() {
		cancel()
		select {
		case <-loopDone:
		case <-time.After(2 * time.Second):
		}
	}()

	if cn := s.currentCommonName(); cn != "reload-first.example.com" {
		t.Fatalf("unexpected initial common name %q", cn)
	}

	// 等循环建立基线后换证书对；写两个文件非原子，中途可能出现
	// key/cert 不匹配的中间态，循环应下个 tick 自愈。
	time.Sleep(3 * checkRate)
	newCertPEM, newKeyPEM := genSelfSignedCertPEM(t, "reload-second.example.com", 2)
	if err := os.WriteFile(certPath, newCertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, newKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.currentCommonName() == "reload-second.example.com" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("key pair not hot-reloaded within 3s, common name still %q", s.currentCommonName())
}

// errorCountLogger 统计携带特定标记的 Error 日志条数（checkKeyPairLoop 的
// 重试错误是唯一打点来源），用于观测重试频率。
type errorCountLogger struct {
	*log.EmptyLogger
	marker string
	mu     sync.Mutex
	count  int
}

func (l *errorCountLogger) Error(v ...interface{}) {
	l.record(fmt.Sprint(v...))
}

func (l *errorCountLogger) Errorf(format string, v ...interface{}) {
	l.record(fmt.Sprintf(format, v...))
}

func (l *errorCountLogger) record(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.Contains(msg, l.marker) {
		l.count++
	}
}

func (l *errorCountLogger) errorCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

// TestCheckKeyPairLoopRetryPacedByTicker busy-loop 回归：key 文件持续不可读时，
// 重试必须受 ticker 节奏约束（~4 个 tick 周期内错误次数个位数），修复前
// 该路径绕过 select 无限自旋，同窗口内次数是修复后的几个数量级。
func TestCheckKeyPairLoopRetryPacedByTicker(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.pem")

	counter := &errorCountLogger{EmptyLogger: &log.EmptyLogger{}, marker: "tls failed to check key"}
	log.RegisterLogger(counter)
	defer log.RegisterLogger(&log.EmptyLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newCheckLoopServer(ctx)

	const checkRate = 50 * time.Millisecond
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.checkKeyPairLoop(checkRate, missingPath, missingPath, "")
	}()

	time.Sleep(4*checkRate + 25*time.Millisecond)
	n := counter.errorCount()
	if n == 0 {
		t.Fatal("expected at least one retry against missing key file")
	}
	if n > 10 {
		t.Fatalf("key file check retried %d times in ~4 tick periods, busy loop regression (want <= 10)", n)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("checkKeyPairLoop did not exit after ctx cancel")
	}
}

// TestCheckKeyPairLoopExitOnContextDone 退出路径：正常巡检若干 tick 后
// cancel，循环应及时返回。
func TestCheckKeyPairLoopExitOnContextDone(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "server.crt")
	keyPath := filepath.Join(t.TempDir(), "server.key")
	certPEM, keyPEM := genSelfSignedCertPEM(t, "exit.example.com", 1)
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newCheckLoopServer(ctx)

	const checkRate = 50 * time.Millisecond
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.checkKeyPairLoop(checkRate, keyPath, certPath, "")
	}()

	time.Sleep(2 * checkRate)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("checkKeyPairLoop did not exit after ctx cancel")
	}
}
