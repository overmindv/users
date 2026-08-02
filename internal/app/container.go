package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/config"
	"github.com/overmindv/users/internal/pkg/singleton"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
)

// Container хранит собранные зависимости Users для runtime-слоя.
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

// NewContainer создаёт dependency container, открывает БД, настраивает JWT и выполняет bootstrap суперпользователя.
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
	// Bootstrap выполняется при сборке container, чтобы первый админ был доступен до старта HTTP-server.
	if err := users.EnsureBootstrapSuperuser(ctx, usecase.BootstrapSuperuserInput{
		Email:     cfg.Bootstrap.SuperuserEmail,
		Password:  cfg.Bootstrap.SuperuserPassword,
		Username:  cfg.Bootstrap.SuperuserUsername,
		FirstName: cfg.Bootstrap.SuperuserFirstName,
		LastName:  cfg.Bootstrap.SuperuserLastName,
	}); err != nil {
		db.Close()
		return nil, err
	}

	return &Container{
		Config: cfg,
		Log:    log,
		DB:     db,
		JWT:    jwtManager,
		Users:  users,
	}, nil
}

// Close закрывает внешние ресурсы container.
func (c *Container) Close() { c.DB.Close() }

// uuidGenerator создаёт UUID для новых пользователей.
type uuidGenerator struct{}

// New возвращает новый UUID string.
func (uuidGenerator) New() string { return uuid.NewString() }

// systemClock отдаёт текущее UTC-время для доменной логики.
type systemClock struct{}

// Now возвращает текущее время в UTC.
func (systemClock) Now() time.Time { return time.Now().UTC() }
