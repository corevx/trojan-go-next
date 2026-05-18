package metric

import "sync/atomic"

type Counter struct {
	value uint64
	help  string
}

func (c *Counter) Inc() {
	atomic.AddUint64(&c.value, 1)
}

func (c *Counter) Add(n uint64) {
	atomic.AddUint64(&c.value, n)
}

func (c *Counter) Value() uint64 {
	return atomic.LoadUint64(&c.value)
}

func (c *Counter) Help() string {
	return c.help
}

type Gauge struct {
	value int64
	help  string
}

func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

func (g *Gauge) Set(n int64) {
	atomic.StoreInt64(&g.value, n)
}

func (g *Gauge) Value() int64 {
	return atomic.LoadInt64(&g.value)
}

func (g *Gauge) Help() string {
	return g.help
}

type Histogram struct {
	mu      chan struct{}
	buckets []float64
	counts  []uint64
	sum     float64
	total   uint64
	help    string
}

func newHistogram(name, help string, bounds []float64) *Histogram {
	h := &Histogram{
		buckets: make([]float64, len(bounds)),
		counts:  make([]uint64, len(bounds)+1),
		help:    help,
	}
	copy(h.buckets, bounds)
	return h
}

func (h *Histogram) Observe(v float64) {
	for i, b := range h.buckets {
		if v <= b {
			atomic.AddUint64(&h.counts[i], 1)
		}
	}
	atomic.AddUint64(&h.counts[len(h.buckets)], 1)
	atomic.AddUint64(&h.total, 1)
}

func (h *Histogram) Buckets() []float64 {
	return h.buckets
}

func (h *Histogram) Counts() []uint64 {
	out := make([]uint64, len(h.counts))
	for i := range h.counts {
		out[i] = atomic.LoadUint64(&h.counts[i])
	}
	return out
}

func (h *Histogram) Total() uint64 {
	return atomic.LoadUint64(&h.total)
}

func (h *Histogram) Help() string {
	return h.help
}
