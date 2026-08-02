package usecase

import (
	"context"
	"io"
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
	Update(context.Context, *domain.User) error
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

// UserMediaStorage задаёт будущий contract хранения пользовательских медиа без привязки к файловой системе или cloud provider.
type UserMediaStorage interface {
	Upload(ctx context.Context, userID, contentType string, content io.Reader) (location string, err error)
	Delete(ctx context.Context, location string) error
}
