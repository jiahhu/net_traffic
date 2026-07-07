package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"nettraffic/internal/collector"
	"nettraffic/internal/config"
	"nettraffic/internal/store"
)

func TestDailyCustomRange(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Config{Mock: true}
	monitor, err := collector.New(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db, monitor, "test")

	req := httptest.NewRequest(http.MethodGet, "/api/daily?start=2026-06-07&end=2026-07-06", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/daily?start=2026-07-06&end=2025-07-06", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid range status=%d body=%s", res.Code, res.Body.String())
	}
}
