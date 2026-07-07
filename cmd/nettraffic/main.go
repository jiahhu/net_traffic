package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nettraffic/internal/collector"
	"nettraffic/internal/config"
	"nettraffic/internal/store"
	webserver "nettraffic/internal/web"
)

var version = "dev"

func main() {
	cfg := config.Parse()
	db, err := store.Open(cfg.Database)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	monitor, err := collector.New(cfg, db)
	if err != nil {
		log.Fatalf("initialize collector: %v", err)
	}
	go monitor.Run(ctx)

	handler := webserver.New(cfg, db, monitor, version)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("nettraffic %s listening on %s (interface %s)", version, cfg.Listen, monitor.Interface())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
