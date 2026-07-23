package auth

import (
	"context"
	"strings"

	"github.com/overmindv/arcee/internal/domain"
)

// userIDKey задаёт приватный ключ context для user ID.
type userIDKey struct{}

// ContextWithUserID добавляет user ID в request context после проверки JWT.
func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserIDFromContext возвращает user ID из context или ошибку авторизации.
func UserIDFromContext(ctx context.Context) (string, error) {
	id, ok := ctx.Value(userIDKey{}).(string)
	if !ok || id == "" {
		return "", domain.ErrUnauthorized
	}

	return id, nil
}

// BearerToken извлекает JWT из Authorization header в формате Bearer.
func BearerToken(header string) (string, error) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", domain.ErrUnauthorized
	}

	return parts[1], nil
}
