package usecase

import (
	"context"
	"time"

	"github.com/overmindv/users/internal/domain"
)

// UserRepository задаёт storage contract для пользователей.
type UserRepository interface {
	Create(context.Context, *domain.User) error
	GetByID(context.Context, string) (*domain.User, error)
	GetByEmail(context.Context, domain.Email) (*domain.User, error)
	GetByUsername(context.Context, domain.Username) (*domain.User, error)
	List(context.Context, string, int, int) ([]*domain.User, error)
	ListPublic(context.Context, string, int, int) ([]*domain.User, error)
	Update(context.Context, *domain.User) error
	SetAvatar(context.Context, *domain.User) error
	UpdateRoles(context.Context, *domain.User) error
	SoftDelete(context.Context, *domain.User) error
}

// PasswordHasher изолирует usecase от конкретного способа хранения паролей.
type PasswordHasher interface {
	Hash(string) (string, error)
	Compare(hash, password string) error
}

// TokenManager задаёт contract выпуска и разбора access token.
type TokenManager interface {
	Issue(userID string, roles []string) (string, time.Time, error)
	Parse(token string) (string, error)
}

// IDGenerator создаёт новые идентификаторы пользователей.
type IDGenerator interface{ New() string }

// Clock возвращает текущее время для доменной логики и тестов.
type Clock interface{ Now() time.Time }

// UserMediaStorage проверяет готовый пользовательский аватар через Media API.
type UserMediaStorage interface {
	ValidateAvatar(ctx context.Context, userID, fileID string) error
}
