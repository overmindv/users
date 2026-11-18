package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config объединяет бизнес-настройки Users: JWT, Media, bootstrap и защиту входа.
// Инфраструктурный конфиг (HTTP, PostgreSQL, логирование, метрики) владеет parker.
type Config struct {
	Profile       string
	JWT           JWT
	Media         Media
	Bootstrap     Bootstrap
	LoginThrottle LoginThrottle
	ReviewGrants  ReviewGrants
}

type ReviewGrants struct {
	EntitiesURL string
	ReadToken   string
}

// JWT описывает настройки выпуска access token.
type JWT struct {
	Secret   string
	Issuer   string
	Lifetime time.Duration
}

// Media описывает внутреннее подключение Users к Media.
type Media struct {
	URL        string
	Token      string
	Timeout    time.Duration
	WorkerPoll time.Duration
}

// Bootstrap описывает данные первого суперпользователя.
type Bootstrap struct {
	SuperuserEmail     string
	SuperuserPassword  string
	SuperuserUsername  string
	SuperuserFirstName string
	SuperuserLastName  string
}

// LoginThrottle описывает защиту от перебора пароля при входе.
type LoginThrottle struct {
	MaxAttempts   int
	IPMaxAttempts int
	Window        time.Duration
}

// Load читает бизнес-конфигурацию Users из environment и валидирует обязательные значения.
func Load() (Config, error) {
	cfg := Config{
		Profile:      env("USERS_PROFILE", "local"),
		ReviewGrants: ReviewGrants{EntitiesURL: os.Getenv("USERS_ENTITIES_URL"), ReadToken: os.Getenv("USERS_REVIEW_GRANTS_TOKEN")},
		JWT: JWT{
			Secret:   env("JWT_SECRET", "local-development-secret-change-me"),
			Issuer:   env("JWT_ISSUER", "users"),
			Lifetime: envDuration("JWT_TTL", 24*time.Hour),
		},
		Media: Media{
			URL:        env("MEDIA_URL", "http://localhost:8085"),
			Token:      env("MEDIA_USERS_TOKEN", ""),
			Timeout:    envDuration("MEDIA_TIMEOUT", 5*time.Second),
			WorkerPoll: envDuration("USERS_WORKER_POLL_INTERVAL", time.Second),
		},
		Bootstrap: Bootstrap{
			SuperuserEmail:     env("BOOTSTRAP_SUPERUSER_EMAIL", ""),
			SuperuserPassword:  env("BOOTSTRAP_SUPERUSER_PASSWORD", ""),
			SuperuserUsername:  env("BOOTSTRAP_SUPERUSER_USERNAME", "superadmin"),
			SuperuserFirstName: env("BOOTSTRAP_SUPERUSER_FIRST_NAME", "Super"),
			SuperuserLastName:  env("BOOTSTRAP_SUPERUSER_LAST_NAME", "Admin"),
		},
		LoginThrottle: LoginThrottle{
			MaxAttempts:   envInt("LOGIN_MAX_ATTEMPTS", 5),
			IPMaxAttempts: envInt("LOGIN_IP_MAX_ATTEMPTS", 100),
			Window:        envDuration("LOGIN_THROTTLE_WINDOW", 15*time.Minute),
		},
	}

	switch cfg.Profile {
	case "local", "test", "staging", "production":
	default:
		return Config{}, fmt.Errorf("invalid USERS_PROFILE")
	}
	if cfg.Profile != "local" {
		if cfg.Bootstrap.SuperuserEmail != "" || cfg.Bootstrap.SuperuserPassword != "" {
			return Config{}, fmt.Errorf("bootstrap credentials require local profile")
		}
		if cfg.JWT.Secret == "local-development-secret-change-me" {
			return Config{}, fmt.Errorf("development JWT secret requires local profile")
		}
	}
	// Лимиты не должны незаметно отключаться из-за ошибки конфигурации.
	for _, setting := range []struct {
		name   string
		target *int
		max    int
	}{
		{"LOGIN_MAX_ATTEMPTS", &cfg.LoginThrottle.MaxAttempts, 10000},
		{"LOGIN_IP_MAX_ATTEMPTS", &cfg.LoginThrottle.IPMaxAttempts, 100000},
	} {
		if raw, present := os.LookupEnv(setting.name); present {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > setting.max {
				return Config{}, fmt.Errorf("%s is out of range", setting.name)
			}
			*setting.target = value
		}
	}
	if raw, present := os.LookupEnv("LOGIN_THROTTLE_WINDOW"); present {
		value, err := time.ParseDuration(raw)
		if err != nil || value < time.Second || value > 24*time.Hour {
			return Config{}, fmt.Errorf("LOGIN_THROTTLE_WINDOW is out of range")
		}
		cfg.LoginThrottle.Window = value
	}

	if cfg.JWT.Secret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET must not be empty")
	}
	if cfg.JWT.Lifetime <= 0 {
		return Config{}, fmt.Errorf("JWT_TTL must be positive")
	}
	if cfg.Media.URL == "" || cfg.Media.Token == "" {
		return Config{}, fmt.Errorf("MEDIA_URL и MEDIA_USERS_TOKEN обязательны")
	}
	review := cfg.ReviewGrants
	if review.ReadToken != "" || review.EntitiesURL != "" {
		if review.EntitiesURL == "" || len(review.ReadToken) < 32 || len(review.ReadToken) > 256 ||
			strings.IndexFunc(review.ReadToken, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 ||
			review.ReadToken == cfg.JWT.Secret || review.ReadToken == cfg.Media.Token {
			return Config{}, fmt.Errorf("review grants require USERS_ENTITIES_URL and a separate USERS_REVIEW_GRANTS_TOKEN")
		}
	}

	return cfg, nil
}

// env возвращает значение environment variable или fallback.
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

// envInt читает целое число из environment variable и возвращает fallback при ошибке формата.
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, ""))
	if err != nil {
		return fallback
	}

	return value
}

// envDuration читает duration из environment variable и возвращает fallback при ошибке формата.
func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(env(key, fallback.String()))
	if err != nil {
		return fallback
	}

	return value
}
