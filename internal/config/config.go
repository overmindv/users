package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config объединяет настройки HTTP, PostgreSQL, JWT и bootstrap-пользователя.
type Config struct {
	HTTP      HTTP
	Database  Database
	JWT       JWT
	Bootstrap Bootstrap
}

// HTTP описывает настройки HTTP-server Arcee.
type HTTP struct {
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

// Database описывает настройки подключения к PostgreSQL.
type Database struct {
	DSN             string
	MaxConnections  int32
	MinConnections  int32
	MaxConnLifetime time.Duration
}

// JWT описывает настройки выпуска access token.
type JWT struct {
	Secret   string
	Issuer   string
	Lifetime time.Duration
}

// Bootstrap описывает данные первого суперпользователя.
type Bootstrap struct {
	SuperuserEmail     string
	SuperuserPassword  string
	SuperuserUsername  string
	SuperuserFirstName string
	SuperuserLastName  string
}

// Load читает конфигурацию Arcee из environment и валидирует обязательные значения.
func Load() (Config, error) {
	port := strings.TrimPrefix(env("PORT", "8080"), ":")

	cfg := Config{
		HTTP: HTTP{
			Address:         ":" + port,
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 5*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Database: Database{
			DSN:             env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/arcee?sslmode=disable"),
			MaxConnections:  int32(envInt("DB_MAX_CONNS", 20)),
			MinConnections:  int32(envInt("DB_MIN_CONNS", 2)),
			MaxConnLifetime: envDuration("DB_MAX_CONN_LIFETIME", 30*time.Minute),
		},
		JWT: JWT{
			Secret:   env("JWT_SECRET", "local-development-secret-change-me"),
			Issuer:   env("JWT_ISSUER", "arcee"),
			Lifetime: envDuration("JWT_TTL", 24*time.Hour),
		},
		Bootstrap: Bootstrap{
			SuperuserEmail:     env("BOOTSTRAP_SUPERUSER_EMAIL", ""),
			SuperuserPassword:  env("BOOTSTRAP_SUPERUSER_PASSWORD", ""),
			SuperuserUsername:  env("BOOTSTRAP_SUPERUSER_USERNAME", "superadmin"),
			SuperuserFirstName: env("BOOTSTRAP_SUPERUSER_FIRST_NAME", "Super"),
			SuperuserLastName:  env("BOOTSTRAP_SUPERUSER_LAST_NAME", "Admin"),
		},
	}

	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, fmt.Errorf("PORT must be a valid TCP port: %w", err)
	}

	if cfg.Database.DSN == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}

	if cfg.JWT.Secret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET must not be empty")
	}

	if cfg.JWT.Lifetime <= 0 {
		return Config{}, fmt.Errorf("JWT_TTL must be positive")
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

// envInt читает integer из environment variable и возвращает fallback при ошибке формата.
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
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
