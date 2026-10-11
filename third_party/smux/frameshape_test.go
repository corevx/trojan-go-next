package smux

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestVerifyConfigFrameShaping(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.KeepAliveDisabled = true
		return c
	}
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"disabled by default", func(c *Config) {}, false},
		{"valid range", func(c *Config) { c.FrameSizeMin = 1024; c.FrameSizeMax = 32768 }, false},
		{"min only", func(c *Config) { c.FrameSizeMin = 1024 }, true},
		{"max only", func(c *Config) { c.FrameSizeMax = 32768 }, true},
		{"min over max", func(c *Config) { c.FrameSizeMin = 2048; c.FrameSizeMax = 1024 }, true},
		{"max over MaxFrameSize", func(c *Config) { c.FrameSizeMin = 1024; c.FrameSizeMax = 40000 }, true},
		{"jitter negative", func(c *Config) { c.KeepAliveJitter = -0.1 }, true},
		{"jitter one", func(c *Config) { c.KeepAliveJitter = 1 }, true},
		{"jitter valid", func(c *Config) { c.KeepAliveJitter = 0.2 }, false},
	}
	for _, tc := range cases {
		c := base()
		tc.mutate(c)
		err := VerifyConfig(c)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: VerifyConfig err=%v wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}

func TestNextFrameSizeDistribution(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	cfg := DefaultConfig()
	cfg.KeepAliveDisabled = true
	cfg.FrameSizeMin = 512
	cfg.FrameSizeMax = 8192
	sess, err := Server(c1, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	const n = 2000
	distinct := make(map[int]int)
	for i := 0; i < n; i++ {
		fs := sess.nextFrameSize()
		if fs < 512 || fs > 8192 {
			t.Fatalf("frame size %d out of [512,8192]", fs)
		}
		distinct[fs]++
	}
	if len(distinct) < n/4 {
		t.Fatalf("distribution too concentrated: %d distinct values in %d draws", len(distinct), n)
	}
	// 未启用随机化时返回固定 MaxFrameSize
	sess2cfg := DefaultConfig()
	sess2cfg.KeepAliveDisabled = true
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()
	sess2, err := Client(p1, sess2cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close()
	if got := sess2.nextFrameSize(); got != sess2cfg.MaxFrameSize {
		t.Fatalf("randomization off: nextFrameSize=%d want %d", got, sess2cfg.MaxFrameSize)
	}
}

// TestFrameShapingRoundTrip 在随机化开启下经完整 stream 收发验证数据完整性。
func TestFrameShapingRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	randCfg := func() *Config {
		c := DefaultConfig()
		c.KeepAliveDisabled = true
		c.FrameSizeMin = 256
		c.FrameSizeMax = 16384
		return c
	}
	cli, err := Client(c1, randCfg())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	srv, err := Server(c2, randCfg())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	payload := make([]byte, 512*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		stream, err := srv.AcceptStream()
		if err != nil {
			t.Error(err)
			return
		}
		got, err := io.ReadAll(stream)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("payload mismatch: got %d bytes", len(got))
		}
	}()

	stream, err := cli.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write(payload); err != nil {
		t.Fatal(err)
	}
	stream.Close()
	wg.Wait()
}

func TestJitterIntervalBounds(t *testing.T) {
	d := 10 * time.Second
	const jitter = 0.2
	sawLow, sawHigh := false, false
	for i := 0; i < 500; i++ {
		got := jitterInterval(d, jitter)
		if got < time.Duration(float64(d)*0.79) || got > time.Duration(float64(d)*1.21) {
			t.Fatalf("jitterInterval out of bounds: %v", got)
		}
		if got < d {
			sawLow = true
		}
		if got > d {
			sawHigh = true
		}
	}
	if !sawLow || !sawHigh {
		t.Fatalf("jitter distribution degenerate: low=%v high=%v", sawLow, sawHigh)
	}
	if got := jitterInterval(d, 0); got != d {
		t.Fatalf("jitter=0 must be identity, got %v", got)
	}
}
