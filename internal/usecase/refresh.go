package usecase

import (
	"context"

	"github.com/overmindv/users/internal/domain"
)

// RefreshTokenManager — опциональное расширение для клиентов с ротацией сессий.
type RefreshTokenManager interface {
	IssueRefreshSession(context.Context, *domain.User) (domain.SessionTokens, error)
	RefreshSession(context.Context, string) (domain.SessionTokens, error)
	RevokeRefreshSession(context.Context, string) error
}

func refreshAuthResult(user *domain.User, tokens domain.SessionTokens) *AuthResult {
	return &AuthResult{User: user, Token: tokens.AccessToken, ExpiresAt: tokens.AccessExpiresAt, RefreshToken: tokens.RefreshToken, RefreshExpiresAt: tokens.RefreshExpiresAt}
}

// RefreshSession обновляет credentials после атомарной проверки family хранилищем.
func (s *UserService) RefreshSession(ctx context.Context, token string) (*AuthResult, error) {
	manager, ok := s.tokens.(RefreshTokenManager)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	tokens, err := manager.RefreshSession(ctx, token)
	if err != nil {
		return nil, err
	}

	return refreshAuthResult(tokens.User, tokens), nil
}

// RevokeRefreshSession не требует действующего access JWT.
func (s *UserService) RevokeRefreshSession(ctx context.Context, token string) error {
	manager, ok := s.tokens.(RefreshTokenManager)
	if !ok {
		return domain.ErrUnauthorized
	}

	return manager.RevokeRefreshSession(ctx, token)
}
