package metric

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestCounter_Inc(t *testing.T) {
	c := &Counter{}
	c.Inc()
	c.Inc()
	if c.Value() != 2 {
		t.Errorf("expected 2, got %d", c.Value())
	}
}

func TestCounter_Add(t *testing.T) {
	c := &Counter{}
	c.Add(10)
	if c.Value() != 10 {
		t.Errorf("expected 10, got %d", c.Value())
	}
}

func TestGauge_IncDec(t *testing.T) {
	g := &Gauge{}
	g.Inc()
	g.Inc()
	g.Dec()
	if g.Value() != 1 {
		t.Errorf("expected 1, got %d", g.Value())
	}
}

func TestGauge_Set(t *testing.T) {
	g := &Gauge{}
	g.Set(42)
	if g.Value() != 42 {
		t.Errorf("expected 42, got %d", g.Value())
	}
}

func TestHistogram_Observe(t *testing.T) {
	h := newHistogram("test", "test", []float64{1, 5, 10})
	h.Observe(0.5)
	h.Observe(3)
	h.Observe(7)
	h.Observe(15)
	if h.Total() != 4 {
		t.Errorf("expected total 4, got %d", h.Total())
	}
}

func TestRegistry_RegisterAndGetAll(t *testing.T) {
	r := &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
	c := r.registerCounter("test_counter", "help")
	c.Inc()
	g := r.registerGauge("test_gauge", "help")
	g.Set(5)
	if r.Counter("test_counter").Value() != 1 {
		t.Error("counter mismatch")
	}
	if r.Gauge("test_gauge").Value() != 5 {
		t.Error("gauge mismatch")
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := &Registry{
		counters: make(map[string]*Counter),
	}
	c1 := r.registerCounter("dup", "first")
	c2 := r.registerCounter("dup", "second")
	if c1 != c2 {
		t.Error("expected same counter on duplicate register")
	}
}

func TestMetrics_ConcurrentAccess(t *testing.T) {
	c := &Counter{}
	g := &Gauge{}
	var wg sync.WaitGroup
	const n = 1000
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func() { defer wg.Done(); c.Inc() }()
		go func() { defer wg.Done(); g.Inc() }()
	}
	wg.Wait()
	if c.Value() != n {
		t.Errorf("counter expected %d, got %d", n, c.Value())
	}
	if g.Value() != int64(n) {
		t.Errorf("gauge expected %d, got %d", n, g.Value())
	}
}

func TestWritePrometheus(t *testing.T) {
	r := &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
	r.registerCounter("http_requests", "Total requests").Inc()
	r.registerGauge("connections", "Active connections").Set(3)
	r.registerHistogram("duration", "Request duration", []float64{0.1, 1.0})
	r.Histogram("duration").Observe(0.05)
	r.Histogram("duration").Observe(0.5)

	var buf bytes.Buffer
	WritePrometheus(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "# TYPE http_requests counter") {
		t.Error("missing counter type line")
	}
	if !strings.Contains(output, "# TYPE connections gauge") {
		t.Error("missing gauge type line")
	}
	if !strings.Contains(output, "# TYPE duration histogram") {
		t.Error("missing histogram type line")
	}
	if !strings.Contains(output, `duration_bucket{le="+Inf"}`) {
		t.Error("missing +Inf bucket")
	}
	if !strings.Contains(output, "duration_count") {
		t.Error("missing histogram count")
	}
}
