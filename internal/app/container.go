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
	"github.com/overmindv/users/internal/delivery/reviewgrants"
	usersmedia "github.com/overmindv/users/internal/media"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/topics"
	"github.com/overmindv/users/internal/usecase"
	"github.com/overmindv/users/internal/worker"
)

const localProfile = "local"

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
	if cfg.JWT.SigningKeys != nil {
		manager, signingErr := auth.NewSigningManager(cfg.JWT.SigningKeys, cfg.JWT.Issuer, cfg.JWT.Audience, cfg.JWT.Lifetime, cfg.JWT.Secret, cfg.JWT.LegacyUntil)
		if signingErr != nil {
			return signingErr
		}
		jwtManager = manager
	}
	app.HTTP().Handle("GET /.well-known/jwks.json", jwtManager.JWKS())

	repository := postgresrepo.NewUserRepository(pool)
	repository.SetLocalCredentials(cfg.Profile == localProfile)
	jwtManager.SetSessionStore(repository)
	jwtManager.SetRefreshStore(repository)
	if err := buildPrivacy(app, pool, repository, jwtManager, cfg); err != nil {
		return err
	}
	app.HTTP().Handle("POST /internal/session/check", jwtManager.SessionHTTP(false))
	app.HTTP().Handle("POST /internal/session/revoke", jwtManager.SessionHTTP(true))
	if cfg.ReviewGrants.ReadToken != "" {
		catalog, err := topics.New(cfg.ReviewGrants.EntitiesURL)
		if err != nil {
			return err
		}
		reviewgrants.Register(app.HTTP(), usecase.NewReviewGrants(repository, catalog), jwtManager, cfg.ReviewGrants.ReadToken)
	}
	mediaClient := usersmedia.New(cfg.Media)
	users := usecase.NewUserServiceWithMedia(
		repository,
		security.NewArgon2IDHasher(),
		jwtManager,
		uuidGenerator{},
		systemClock{},
		mediaClient,
	)
	users.SetLocalCredentials(cfg.Profile == localProfile)
	users.SetPrivacyEnabled(cfg.Privacy.Enabled)
	recovery, recoveryErr := buildRecovery(app, pool, repository, cfg.Recovery)
	if recoveryErr != nil {
		return recoveryErr
	}
	// Общие для всех реплик лимиты по email и доверенному IP.
	throttle := postgresrepo.NewLoginThrottler(pool, cfg.LoginContextSecret, cfg.LoginThrottle.MaxAttempts, cfg.LoginThrottle.IPMaxAttempts, cfg.LoginThrottle.Window)
	users.SetLoginThrottler(throttle)
	app.AddRunnable("login-throttle-cleanup", worker.RunLoginThrottleCleanup(throttle, app.Logger()))

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
		&graphqldelivery.Resolver{Users: users, Recovery: recovery},
		func(h http.Handler) http.Handler {
			return auth.LoginClientIP(cfg.LoginContextSecret, auth.OptionalHTTP(jwtManager, h))
		},
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
