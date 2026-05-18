package monitor

import (
	"context"
	"fmt"
	"net/http"

	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/metric"
)

var readyFunc func() bool

func SetReadyFunc(fn func() bool) {
	readyFunc = fn
}

type Config struct {
	Monitor MonitorConfig `json:"monitor" yaml:"monitor"`
}

func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return new(Config)
	})
}

func RunMonitorServer(ctx context.Context) error {
	cfg := config.FromContext(ctx, Name).(*Config)
	if !cfg.Monitor.Enabled {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz)
	mux.HandleFunc("/metrics", handleMetrics)

	addr := fmt.Sprintf("%s:%d", cfg.Monitor.MonitorHost, cfg.Monitor.MonitorPort)
	log.WithField("addr", addr).Info("monitor server listening")
	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleReadyz(w http.ResponseWriter, r *http.Request) {
	if readyFunc != nil && !readyFunc() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	metric.WritePrometheus(w, metric.Default())
}
