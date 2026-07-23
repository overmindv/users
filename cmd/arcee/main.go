package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/overmindv/arcee/internal/app"
	"github.com/overmindv/arcee/internal/config"
)

// main загружает конфигурацию, собирает container и запускает HTTP runtime Arcee.
func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	container, err := app.NewContainer(ctx, cfg, log)
	if err != nil {
		log.Error("initialize arcee container", "error", err)
		os.Exit(1)
	}
	defer container.Close()

	if err := app.NewRuntime(container).Run(ctx); err != nil {
		log.Error("run service", "error", err)
		os.Exit(1)
	}
}
