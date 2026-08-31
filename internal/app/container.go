package app

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/config"
	graphqldelivery "github.com/overmindv/users/internal/delivery/graphql"
	usersmedia "github.com/overmindv/users/internal/media"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
	"github.com/overmindv/users/internal/worker"
)

// Build выполняет wiring бизнес-зависимостей Users на каркас parker:
// открывает базу, настраивает JWT/media, регистрирует GraphQL-роуты, health-чекки
// и фоновый воркер доставки avatar outbox.
func Build(app *parker.App) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := app.Postgres() // добавляет health-чек "postgres" в /ready
	if err != nil {
		return err
	}

	jwtManager := auth.NewManager(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.Lifetime)

	repository := postgresrepo.NewUserRepository(pool)
	mediaClient := usersmedia.New(cfg.Media)
	users := usecase.NewUserServiceWithMedia(
		repository,
		security.PlainTextHasher{},
		jwtManager,
		uuidGenerator{},
		systemClock{},
		mediaClient,
	)

	// Bootstrap первого администратора выполняется здесь, чтобы админ был доступен до старта HTTP-server.
	ctx := context.Background()
	if err := users.EnsureBootstrapSuperuser(ctx, usecase.BootstrapSuperuserInput{
		Email:     cfg.Bootstrap.SuperuserEmail,
		Password:  cfg.Bootstrap.SuperuserPassword,
		Username:  cfg.Bootstrap.SuperuserUsername,
		FirstName: cfg.Bootstrap.SuperuserFirstName,
		LastName:  cfg.Bootstrap.SuperuserLastName,
	}); err != nil {
		return err
	}

	// Готовность: postgres (через app.Postgres) + доступность Media.
	app.AddHealthCheck("media", parker.HealthCheckFunc(mediaClient.Ready))

	// Фоновый доставщик transactional outbox аватаров.
	avatarWorker := worker.NewAvatar(postgresrepo.NewAvatarOutbox(pool), mediaClient, cfg.Media.WorkerPoll, app.Logger())
	app.AddRunnable("avatar-outbox", avatarWorker.Run)

	// GraphQL-транспорт; JWT применяется к /query и /graphql, но не к /playground.
	graphqldelivery.Register(
		app.HTTP(),
		&graphqldelivery.Resolver{Users: users},
		func(h http.Handler) http.Handler { return auth.OptionalHTTP(jwtManager, h) },
		app.Logger(),
	)
	return nil
}

// uuidGenerator создаёт UUID для новых пользователей.
type uuidGenerator struct{}

// New возвращает новый UUID string.
func (uuidGenerator) New() string { return uuid.NewString() }

// systemClock отдаёт текущее UTC-время для доменной логики.
type systemClock struct{}

// Now возвращает текущее время в UTC.
func (systemClock) Now() time.Time { return time.Now().UTC() }
