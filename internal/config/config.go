package config

import "context"

// Config хранит конфиг, содержащий все rtc переменные, разбитые по группам
type Config struct{}

func NewConfig(ctx context.Context) (*Config, error) {
	return &Config{}, nil
}
