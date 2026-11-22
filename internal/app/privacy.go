package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/config"
	privacydelivery "github.com/overmindv/users/internal/delivery/privacy"
	"github.com/overmindv/users/internal/privacyowner"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
	"github.com/overmindv/users/internal/worker"
)

func buildPrivacy(app *parker.App, pool *pgxpool.Pool, repository *postgresrepo.UserRepository, sessions *auth.Manager, cfg config.Config) error {
	if !cfg.Privacy.Enabled {
		return nil
	}
	producer, err := app.NewProducer()
	if err != nil {
		return err
	}
	service := usecase.NewPrivacy(repository, security.NewArgon2IDHasher())
	throttle := postgresrepo.NewLoginThrottler(pool, "privacy:"+cfg.LoginContextSecret, 5, 50, 15*time.Minute)
	privacydelivery.Register(app.HTTP(), service, repository, sessions, cfg.Privacy.Owners, throttle, cfg.LoginContextSecret)
	app.AddRunnable("account-privacy", worker.NewPrivacy(repository, privacyowner.New(cfg.Privacy.Owners), producer, app.Logger()).Run)

	return nil
}
