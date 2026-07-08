package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/arcee/internal/auth"
	"github.com/overmindv/arcee/internal/config"
	"github.com/overmindv/arcee/internal/pkg/singleton"
	postgresrepo "github.com/overmindv/arcee/internal/repository/postgres"
	"github.com/overmindv/arcee/internal/security"
	"github.com/overmindv/arcee/internal/usecase"
)

type Container struct {
	Config config.Config
	Log    *slog.Logger
	DB     *pgxpool.Pool
	JWT    *auth.Manager
	Users  *usecase.UserService
}

var (
	databaseProvider singleton.Provider[*pgxpool.Pool]
	tokenProvider    singleton.Provider[*auth.Manager]
)

func NewContainer(ctx context.Context, cfg config.Config, log *slog.Logger) (*Container, error) {
	db, err := databaseProvider.Get(func() (*pgxpool.Pool, error) {
		return postgresrepo.Open(ctx, cfg.Database)
	})
	if err != nil {
		return nil, err
	}

	jwtManager, err := tokenProvider.Get(func() (*auth.Manager, error) {
		return auth.NewManager(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.Lifetime), nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	repository := postgresrepo.NewUserRepository(db)

	users := usecase.NewUserService(repository, security.PlainTextHasher{}, jwtManager, uuidGenerator{}, systemClock{})

	return &Container{
		Config: cfg,
		Log:    log,
		DB:     db,
		JWT:    jwtManager,
		Users:  users,
	}, nil
}

func (c *Container) Close() { c.DB.Close() }

type uuidGenerator struct{}

func (uuidGenerator) New() string { return uuid.NewString() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
