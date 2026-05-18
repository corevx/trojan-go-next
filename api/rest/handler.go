package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/p4gefau1t/trojan-go/api"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/metric"
	"github.com/p4gefau1t/trojan-go/statistic"
)

const serviceName = "TROJAN_SERVER_REST"
const clientServiceName = "TROJAN_CLIENT_REST"

func init() {
	api.RegisterHandler(serviceName, runServerRESTAPI)
	api.RegisterHandler(clientServiceName, runClientRESTAPI)
}

type Config struct {
	REST RESTConfig `json:"rest" yaml:"rest"`
}

func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return new(Config)
	})
}

type apiHandler struct {
	auth statistic.Authenticator
	mux  *http.ServeMux
}

func (h *apiHandler) setupRoutes(apiKey string, cors []string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/users", h.authMiddleware(apiKey, h.corsMiddleware(cors, h.handleUsers)))
	mux.HandleFunc("/api/v1/users/", h.authMiddleware(apiKey, h.corsMiddleware(cors, h.handleUserByHash)))
	mux.HandleFunc("/api/v1/traffic/", h.authMiddleware(apiKey, h.corsMiddleware(cors, h.handleTraffic)))
	mux.HandleFunc("/api/v1/stats", h.authMiddleware(apiKey, h.corsMiddleware(cors, h.handleStats)))
	h.mux = mux
}

func (h *apiHandler) authMiddleware(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	return authMiddleware(apiKey)(next).ServeHTTP
}

func (h *apiHandler) corsMiddleware(allowed []string, next http.HandlerFunc) http.HandlerFunc {
	return corsMiddleware(allowed)(next).ServeHTTP
}

func (h *apiHandler) handleUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		users := h.auth.ListUsers()
		result := make([]map[string]interface{}, 0, len(users))
		for _, u := range users {
			sent, recv := u.GetTraffic()
			ssent, srecv := u.GetSpeed()
			result = append(result, map[string]interface{}{
				"hash":       u.Hash(),
				"sent":       sent,
				"recv":       recv,
				"speed_sent": ssent,
				"speed_recv": srecv,
				"ip_count":   u.GetIP(),
				"ip_limit":   u.GetIPLimit(),
			})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"users": result})
	case http.MethodPost:
		var req struct {
			Hash      string `json:"hash"`
			SpeedUp   int    `json:"speed_up"`
			SpeedDown int    `json:"speed_down"`
			IPLimit   int    `json:"ip_limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if req.Hash == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "hash is required"})
			return
		}
		if err := h.auth.AddUser(req.Hash); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_, user := h.auth.AuthUser(req.Hash)
		if user != nil {
			user.SetSpeedLimit(req.SpeedUp, req.SpeedDown)
			user.SetIPLimit(req.IPLimit)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *apiHandler) handleUserByHash(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if hash == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		_, user := h.auth.AuthUser(hash)
		if user == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
			return
		}
		sent, recv := user.GetTraffic()
		ssent, srecv := user.GetSpeed()
		su, sd := user.GetSpeedLimit()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hash":         user.Hash(),
			"sent":         sent,
			"recv":         recv,
			"speed_sent":   ssent,
			"speed_recv":   srecv,
			"speed_limit":  map[string]int{"up": su, "down": sd},
			"ip_count":     user.GetIP(),
			"ip_limit":     user.GetIPLimit(),
		})
	case http.MethodDelete:
		if err := h.auth.DelUser(hash); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	case http.MethodPut:
		var req struct {
			SpeedUp   int `json:"speed_up"`
			SpeedDown int `json:"speed_down"`
			IPLimit   int `json:"ip_limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_, user := h.auth.AuthUser(hash)
		if user == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
			return
		}
		user.SetSpeedLimit(req.SpeedUp, req.SpeedDown)
		user.SetIPLimit(req.IPLimit)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *apiHandler) handleTraffic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, "/api/v1/traffic/")
	_, user := h.auth.AuthUser(hash)
	if user == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}
	sent, recv := user.GetTraffic()
	ssent, srecv := user.GetSpeed()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hash":       hash,
		"sent":       sent,
		"recv":       recv,
		"speed_sent": ssent,
		"speed_recv": srecv,
	})
}

func (h *apiHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	users := h.auth.ListUsers()
	var totalSent, totalRecv uint64
	for _, u := range users {
		s, r := u.GetTraffic()
		totalSent += s
		totalRecv += r
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_connections": metric.Default().Gauge("trojan_active_connections"),
		"total_connections":  metric.Default().Counter("trojan_connections_total"),
		"users":              len(users),
		"total_sent":         totalSent,
		"total_recv":         totalRecv,
	})
}

func runServerRESTAPI(ctx context.Context, auth statistic.Authenticator) error {
	cfg := config.FromContext(ctx, Name).(*Config)
	if !cfg.REST.Enabled {
		return nil
	}
	h := &apiHandler{auth: auth}
	h.setupRoutes(cfg.REST.APIKey, cfg.REST.CORS)

	handler := rateLimitMiddleware(100)(loggingMiddleware(h.mux))
	addr := fmt.Sprintf(":%d", cfg.REST.RESTPort)
	log.WithField("addr", addr).Info("rest api server listening")
	server := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func runClientRESTAPI(ctx context.Context, auth statistic.Authenticator) error {
	cfg := config.FromContext(ctx, Name).(*Config)
	if !cfg.REST.Enabled {
		return nil
	}
	h := &apiHandler{auth: auth}
	h.setupRoutes(cfg.REST.APIKey, cfg.REST.CORS)

	handler := rateLimitMiddleware(100)(loggingMiddleware(h.mux))
	addr := fmt.Sprintf(":%d", cfg.REST.RESTPort)
	log.WithField("addr", addr).Info("rest api client listening")
	server := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}
