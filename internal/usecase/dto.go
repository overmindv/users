package usecase

import (
	"time"

	"github.com/overmindv/arcee/internal/domain"
)

type RegisterInput struct {
	Email     string
	Password  string
	Username  string
	FirstName string
	LastName  string
	BirthDate *time.Time
	Phone     string
}

type LoginInput struct {
	Email    string
	Password string
}

type AuthResult struct {
	User      *domain.User
	Token     string
	ExpiresAt time.Time
}

type UpdateUserInput struct {
	ID             string
	Username       *string
	FirstName      *string
	LastName       *string
	BirthDate      *time.Time
	ClearBirthDate bool
	Phone          *string
}
