package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/overmindv/users/internal/config"
	usersmedia "github.com/overmindv/users/internal/media"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/worker"
)

// main запускает отдельный доставщик transactional outbox аватаров.
func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := postgresrepo.Open(ctx, cfg.Database)
	if err != nil {
		log.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	mediaClient := usersmedia.New(cfg.Media)
	probe := &http.Server{
		Addr:              cfg.Media.WorkerHTTP,
		ReadHeaderTimeout: 3 * time.Second,
	}
	probe.Handler = readiness(pool.Ping, mediaClient.Ready)
	go func() {
		if err := probe.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("worker probe failed", "error", err)
			stop()
		}
	}()
	processor := worker.NewAvatar(postgresrepo.NewAvatarOutbox(pool), mediaClient, cfg.Media.WorkerPoll, log)
	if err := processor.Run(ctx); err != nil {
		log.Error("users worker failed", "error", err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := probe.Shutdown(shutdownContext); err != nil {
		log.Error("shutdown worker probe", "error", err)
	}
}

func readiness(database func(context.Context) error, media func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := database(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)

			return
		}
		if err := media(ctx); err != nil {
			http.Error(w, "media unavailable", http.StatusServiceUnavailable)

			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return mux
}
