package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rate-limiter-service/internal/backend"
	"rate-limiter-service/internal/config"
	httpapi "rate-limiter-service/internal/http"
)

func main() {
	cfg := config.Load()

	var (
		store backend.Backend
		err   error
	)

	switch cfg.Backend {
	case "redis":
		store, err = backend.NewRedisBackend(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	default:
		store = backend.NewMemoryBackend()
	}
	if err != nil {
		log.Fatalf("backend init failed: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("backend close failed: %v", err)
		}
	}()

	handler := httpapi.NewHandler(store)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.Routes(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("rate limiter listening on :%s (backend=%s)", cfg.Port, cfg.Backend)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	waitForShutdown(server)
}

func waitForShutdown(server *http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
