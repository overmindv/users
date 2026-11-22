package app

import (
	"encoding/base64"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/config"
	"github.com/overmindv/users/internal/recoverymail"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
	"github.com/overmindv/users/internal/worker"
)

func buildRecovery(app *parker.App, pool *pgxpool.Pool, repository *postgresrepo.UserRepository, cfg config.Recovery) (*usecase.Recovery, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	cipher, err := security.NewRecoveryMailCipher(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	secret := base64.StdEncoding.EncodeToString(cfg.EncryptionKey)
	requests := postgresrepo.NewLoginThrottler(pool, "recovery-request:"+secret, 5, 100, 15*time.Minute)
	consumes := postgresrepo.NewLoginThrottler(pool, "recovery-consume:"+secret, 5, 20, 15*time.Minute)
	queue := postgresrepo.NewRecoveryOutbox(pool)
	sender := recoverymail.NewSMTP(cfg)
	app.AddRunnable("recovery-mail", worker.NewRecovery(queue, cipher, sender, app.Logger()).Run)

	return usecase.NewRecovery(repository, cipher, security.NewArgon2IDHasher(), requests, consumes), nil
}
