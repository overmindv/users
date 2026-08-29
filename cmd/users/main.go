package main

import (
	"os"

	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/app"
)

// main запускает Users на каркасе parker: вся инфраструктура (конфиг, HTTP,
// postgres+миграции, логирование, метрики, graceful shutdown) — внутри parker,
// здесь только бизнес-lогика (см. app.Build).
func main() {
	os.Exit(parker.Main(run, parker.WithAppName("users")))
}

// run регистрирует бизнес-логику Users на каркас parker.
func run(application *parker.App) error {
	return app.Build(application)
}
