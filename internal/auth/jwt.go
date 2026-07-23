package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/overmindv/arcee/internal/domain"
)

// Manager выпускает и проверяет JWT для пользователей Arcee.
type Manager struct {
	secret   []byte
	issuer   string
	lifetime time.Duration
	now      func() time.Time
}

// Claims описывает JWT claims, которые нужны gateway для проверки ролей.
type Claims struct {
	Roles []string `json:"roles"`
	jwt.RegisteredClaims
}

// NewManager создаёт JWT manager с HS256 secret, issuer и lifetime.
func NewManager(secret, issuer string, lifetime time.Duration) *Manager {
	return &Manager{
		secret:   []byte(secret),
		issuer:   issuer,
		lifetime: lifetime,
		now:      time.Now,
	}
}

// Issue выпускает JWT для пользователя и возвращает token вместе со временем истечения.
func (m *Manager) Issue(userID string, roles []string) (string, time.Time, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.lifetime)
	claims := Claims{
		Roles: append([]string(nil), roles...),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}

	return value, expiresAt, nil
}

// Parse проверяет JWT и возвращает user ID из subject.
func (m *Manager) Parse(value string) (string, error) {
	claims := &jwt.RegisteredClaims{}

	token, err := jwt.ParseWithClaims(value, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}

		// Проверяем алгоритм до возврата secret, чтобы не принять token с неподдерживаемой подписью.
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(m.now))
	if err != nil || !token.Valid || claims.Subject == "" {
		return "", domain.ErrUnauthorized
	}

	return claims.Subject, nil
}
