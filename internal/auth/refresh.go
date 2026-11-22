package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

const refreshLifetime = 30 * 24 * time.Hour

// RefreshStore атомарно хранит и меняет только хэши refresh credentials.
type RefreshStore interface {
	CreateRefreshSession(context.Context, *domain.User, string, string, []byte, time.Time, time.Time) (int64, error)
	RotateRefreshSession(context.Context, []byte, []byte, string, time.Time) (domain.RefreshGrant, error)
	RevokeRefreshSession(context.Context, []byte) error
}

// SetRefreshStore подключает хранилище ротации; production также требует SessionStore.
func (m *Manager) SetRefreshStore(store RefreshStore) { m.refresh = store }

func newRefreshToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	return token, hash[:], nil
}

func refreshHash(token string) ([]byte, error) {
	if len(token) != 43 {
		return nil, domain.ErrUnauthorized
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, domain.ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(token))

	return hash[:], nil
}

// IssueRefreshSession создаёт отдельную family для каждого входа с opt-in.
func (m *Manager) IssueRefreshSession(ctx context.Context, user *domain.User) (domain.SessionTokens, error) {
	if m.refresh == nil {
		return domain.SessionTokens{}, domain.ErrUnauthorized
	}
	token, hash, err := newRefreshToken()
	if err != nil {
		return domain.SessionTokens{}, err
	}
	now := m.now().UTC()
	refreshExpiry := now.Add(refreshLifetime)
	accessExpiry := now.Add(m.lifetime)
	if refreshExpiry.Before(accessExpiry) {
		accessExpiry = refreshExpiry
	}
	id := uuid.NewString()
	version, err := m.refresh.CreateRefreshSession(ctx, user, uuid.NewString(), id, hash, accessExpiry, refreshExpiry)
	if err != nil {
		return domain.SessionTokens{}, err
	}

	return m.signRefreshGrant(id, token, domain.RefreshGrant{User: user, UserID: user.ID(), Roles: user.Roles(), SecurityVersion: version, AccessExpiresAt: accessExpiry, RefreshExpiresAt: refreshExpiry})
}

// RefreshSession потребляет одноразовый credential независимо от срока access JWT.
func (m *Manager) RefreshSession(ctx context.Context, token string) (domain.SessionTokens, error) {
	if m.refresh == nil {
		return domain.SessionTokens{}, domain.ErrUnauthorized
	}
	hash, err := refreshHash(token)
	if err != nil {
		return domain.SessionTokens{}, err
	}
	next, nextHash, err := newRefreshToken()
	if err != nil {
		return domain.SessionTokens{}, err
	}
	id := uuid.NewString()
	grant, err := m.refresh.RotateRefreshSession(ctx, hash, nextHash, id, m.now().UTC().Add(m.lifetime))
	if err != nil {
		return domain.SessionTokens{}, err
	}

	return m.signRefreshGrant(id, next, grant)
}

func (m *Manager) signRefreshGrant(id, token string, grant domain.RefreshGrant) (domain.SessionTokens, error) {
	claims := Claims{Roles: grant.Roles, SessionVersion: grant.SecurityVersion, RegisteredClaims: jwt.RegisteredClaims{ID: id, Subject: grant.UserID, Issuer: m.issuer, IssuedAt: jwt.NewNumericDate(m.now().UTC()), ExpiresAt: jwt.NewNumericDate(grant.AccessExpiresAt)}}
	access, err := m.sign(claims)
	if err != nil {
		return domain.SessionTokens{}, err
	}

	return domain.SessionTokens{User: grant.User, UserID: grant.UserID, AccessToken: access, AccessExpiresAt: grant.AccessExpiresAt, RefreshToken: token, RefreshExpiresAt: grant.RefreshExpiresAt}, nil
}

// RevokeRefreshSession отзывает family даже после истечения access JWT.
func (m *Manager) RevokeRefreshSession(ctx context.Context, token string) error {
	if m.refresh == nil {
		return domain.ErrUnauthorized
	}
	hash, err := refreshHash(token)
	if err != nil {
		return err
	}

	return m.refresh.RevokeRefreshSession(ctx, hash)
}
