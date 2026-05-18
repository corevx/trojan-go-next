package metric

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

var defaultRegistry = &Registry{
	counters:   make(map[string]*Counter),
	gauges:     make(map[string]*Gauge),
	histograms: make(map[string]*Histogram),
}

type Registry struct {
	mu         sync.Mutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

func Default() *Registry {
	return defaultRegistry
}

func RegisterCounter(name, help string) *Counter {
	return defaultRegistry.registerCounter(name, help)
}

func RegisterGauge(name, help string) *Gauge {
	return defaultRegistry.registerGauge(name, help)
}

func RegisterHistogram(name, help string, buckets []float64) *Histogram {
	return defaultRegistry.registerHistogram(name, help, buckets)
}

func (r *Registry) registerCounter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{help: help}
	r.counters[name] = c
	return c
}

func (r *Registry) registerGauge(name, help string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{help: help}
	r.gauges[name] = g
	return g
}

func (r *Registry) registerHistogram(name, help string, buckets []float64) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok {
		return h
	}
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	h := newHistogram(name, help, sorted)
	r.histograms[name] = h
	return h
}

func (r *Registry) Counter(name string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counters[name]
}

func (r *Registry) Gauge(name string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gauges[name]
}

func (r *Registry) Histogram(name string) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.histograms[name]
}

func WritePrometheus(w io.Writer, reg *Registry) {
	if reg == nil {
		reg = defaultRegistry
	}
	reg.mu.Lock()
	counters := make(map[string]*Counter, len(reg.counters))
	for k, v := range reg.counters {
		counters[k] = v
	}
	gauges := make(map[string]*Gauge, len(reg.gauges))
	for k, v := range reg.gauges {
		gauges[k] = v
	}
	histograms := make(map[string]*Histogram, len(reg.histograms))
	for k, v := range reg.histograms {
		histograms[k] = v
	}
	reg.mu.Unlock()

	for _, name := range sortedKeys(counters) {
		c := counters[name]
		fmt.Fprintf(w, "# HELP %s %s\n", name, c.Help())
		fmt.Fprintf(w, "# TYPE %s counter\n", name)
		fmt.Fprintf(w, "%s %d\n", name, c.Value())
	}

	for _, name := range sortedKeys(gauges) {
		g := gauges[name]
		fmt.Fprintf(w, "# HELP %s %s\n", name, g.Help())
		fmt.Fprintf(w, "# TYPE %s gauge\n", name)
		fmt.Fprintf(w, "%s %d\n", name, g.Value())
	}

	for _, name := range sortedKeys(histograms) {
		h := histograms[name]
		fmt.Fprintf(w, "# HELP %s %s\n", name, h.Help())
		fmt.Fprintf(w, "# TYPE %s histogram\n", name)
		counts := h.Counts()
		buckets := h.Buckets()
		var cumulative uint64
		for i, b := range buckets {
			cumulative += counts[i]
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, b, cumulative)
		}
		cumulative += counts[len(buckets)]
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, cumulative)
		fmt.Fprintf(w, "%s_count %d\n", name, h.Total())
	}
}

func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sanitizeMetricName replaces invalid characters for Prometheus metric names.
func sanitizeMetricName(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
}
