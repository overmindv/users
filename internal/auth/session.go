package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

// SessionStore хранит сессии и проверяет текущую версию безопасности пользователя.
type SessionStore interface {
	CreateSession(context.Context, *domain.User, string, time.Time) (int64, error)
	CheckSession(context.Context, string, string, int64) ([]string, error)
	RevokeSession(context.Context, string, string) error
}

// SetSessionStore включает обязательную проверку durable sessions.
func (m *Manager) SetSessionStore(store SessionStore) { m.sessions = store }

// IssueUser сохраняет сессию только для актуального снимка учётной записи.
func (m *Manager) IssueUser(ctx context.Context, user *domain.User) (string, time.Time, error) {
	if m.sessions == nil {
		return m.Issue(user.ID(), user.Roles())
	}
	now := m.now().UTC()
	expires := now.Add(m.lifetime)
	id := uuid.NewString()
	version, err := m.sessions.CreateSession(ctx, user, id, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	claims := Claims{Roles: user.Roles(), SessionVersion: version, RegisteredClaims: jwt.RegisteredClaims{ID: id, Subject: user.ID(), Issuer: m.issuer, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires)}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return token, expires, err
}

func (m *Manager) parseClaims(value string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(value, claims, func(token *jwt.Token) (any, error) { return m.secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(m.now))
	if err != nil || token == nil || !token.Valid || claims.Subject == "" {
		return nil, domain.ErrUnauthorized
	}
	return claims, nil
}

// Validate проверяет подпись, срок, отзыв и актуальные роли; ошибки storage не разрешают доступ.
func (m *Manager) Validate(ctx context.Context, value string) (*Claims, error) {
	claims, err := m.parseClaims(value)
	if err != nil {
		return nil, err
	}
	if m.sessions != nil {
		if _, err = uuid.Parse(claims.ID); err != nil || claims.SessionVersion < 1 {
			return nil, domain.ErrUnauthorized
		}
		claims.Roles, err = m.sessions.CheckSession(ctx, claims.Subject, claims.ID, claims.SessionVersion)
		if err != nil {
			return nil, err
		}
	}
	return claims, nil
}

// Revoke идемпотентно отзывает только предъявленную подписанную сессию.
func (m *Manager) Revoke(ctx context.Context, value string) error {
	claims, err := m.parseClaims(value)
	if err != nil {
		return err
	}
	if m.sessions == nil || claims.ID == "" {
		return domain.ErrUnauthorized
	}
	if _, err = uuid.Parse(claims.ID); err != nil {
		return domain.ErrUnauthorized
	}
	return m.sessions.RevokeSession(ctx, claims.Subject, claims.ID)
}

// SessionHTTP — внутренний контракт gateway без персональных данных профиля.
func (m *Manager) SessionHTTP(revoke bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token, err := BearerToken(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if revoke {
			if err = m.Revoke(ctx, token); err != nil {
				sessionError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		claims, err := m.Validate(ctx, token)
		if err != nil {
			sessionError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			UserID    string    `json:"user_id"`
			Roles     []string  `json:"roles"`
			SessionID string    `json:"session_id"`
			ExpiresAt time.Time `json:"expires_at"`
		}{claims.Subject, claims.Roles, claims.ID, claims.ExpiresAt.Time})
	})
}

func sessionError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, domain.ErrUnauthorized) {
		code = http.StatusUnauthorized
	}
	http.Error(w, "session unavailable", code)
}
