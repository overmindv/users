package usecase

import (
	"context"
	"io"
	"time"

	"github.com/overmindv/arcee/internal/domain"
)

type UserRepository interface {
	Create(context.Context, *domain.User) error
	GetByID(context.Context, string) (*domain.User, error)
	GetByEmail(context.Context, domain.Email) (*domain.User, error)
	List(context.Context, int, int) ([]*domain.User, error)
	Update(context.Context, *domain.User) error
	SoftDelete(context.Context, *domain.User) error
}

// PasswordHasher keeps password storage replaceable. The prototype binds it to
// PlainTextHasher; switching to bcrypt does not change the use cases.
type PasswordHasher interface {
	Hash(string) (string, error)
	Compare(hash, password string) error
}

type TokenManager interface {
	Issue(userID string) (string, time.Time, error)
	Parse(token string) (string, error)
}

type IDGenerator interface{ New() string }
type Clock interface{ Now() time.Time }

// UserMediaStorage is a future-facing port for avatars and profile media. It
// deliberately exposes no filesystem or cloud-vendor types to the use cases.
type UserMediaStorage interface {
	Upload(ctx context.Context, userID, contentType string, content io.Reader) (location string, err error)
	Delete(ctx context.Context, location string) error
}
