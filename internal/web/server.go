package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nettraffic/internal/collector"
	"nettraffic/internal/config"
	"nettraffic/internal/store"
)

//go:embed static/*
var assets embed.FS

type server struct {
	cfg     config.Config
	store   *store.Store
	monitor *collector.Collector
	version string
}

func New(cfg config.Config, db *store.Store, monitor *collector.Collector, version string) http.Handler {
	s := &server{cfg: cfg, store: db, monitor: monitor, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("GET /api/series", s.series)
	mux.HandleFunc("GET /api/daily", s.daily)
	mux.HandleFunc("GET /api/destinations", s.destinations)
	mux.HandleFunc("GET /api/live", s.live)
	static, _ := fs.Sub(assets, "static")
	mux.Handle("/", http.FileServer(http.FS(static)))
	return s.headers(s.auth(mux))
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	todayRX, todayTX, err := s.store.Totals(r.Context(), s.monitor.Interface(), now)
	if err != nil {
		writeError(w, err)
		return
	}
	monthRX, monthTX, err := s.store.Totals(r.Context(), s.monitor.Interface(), now.AddDate(0, 0, -29))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  s.monitor.Latest(),
		"today":   map[string]int64{"rxBytes": todayRX, "txBytes": todayTX},
		"month":   map[string]int64{"rxBytes": monthRX, "txBytes": monthTX},
		"version": s.version,
	})
}

func (s *server) series(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	duration, bucket := 24*time.Hour, int64(300)
	switch rangeName {
	case "week":
		duration, bucket = 7*24*time.Hour, 1800
	case "month":
		duration, bucket = 30*24*time.Hour, 7200
	default:
		rangeName = "day"
	}
	points, err := s.store.Series(r.Context(), s.monitor.Interface(), time.Now().Add(-duration), bucket)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rangeName, "bucketSeconds": bucket, "points": points})
}

func (s *server) daily(w http.ResponseWriter, r *http.Request) {
	startValue, endValue := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if startValue != "" || endValue != "" {
		if startValue == "" || endValue == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start and end are both required"})
			return
		}
		start, startErr := time.ParseInLocation("2006-01-02", startValue, time.Local)
		end, endErr := time.ParseInLocation("2006-01-02", endValue, time.Local)
		if startErr != nil || endErr != nil || end.Before(start) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date range"})
			return
		}
		if end.Sub(start) > 366*24*time.Hour {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date range cannot exceed 366 days"})
			return
		}
		rows, err := s.store.DailyRange(r.Context(), s.monitor.Interface(), start, end)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"start": startValue, "end": endValue, "days": rows})
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 7 || days > 365 {
		days = 30
	}
	rows, err := s.store.Daily(r.Context(), s.monitor.Interface(), days)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": rows})
}

func (s *server) destinations(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	duration := 24 * time.Hour
	switch rangeName {
	case "hour":
		duration = time.Hour
	case "week":
		duration = 7 * 24 * time.Hour
	case "month":
		duration = 30 * 24 * time.Hour
	default:
		rangeName = "day"
	}
	rows, err := s.store.Destinations(r.Context(), time.Now().Add(-duration), 12)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rangeName, "destinations": rows})
}

func (s *server) live(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch, cancel := s.monitor.Subscribe()
	defer cancel()
	for {
		select {
		case status, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(status)
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) auth(next http.Handler) http.Handler {
	if s.cfg.Username == "" && s.cfg.Password == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		validUser := subtle.ConstantTimeCompare([]byte(user), []byte(s.cfg.Username)) == 1
		validPass := subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.Password)) == 1
		if !ok || !validUser || !validPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="NetTraffic", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
