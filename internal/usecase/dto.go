package usecase

import (
	"time"

	"github.com/overmindv/users/internal/domain"
)

// RegisterInput описывает данные для регистрации обычного пользователя.
type RegisterInput struct {
	Email     string
	Password  string
	Username  string
	FirstName string
	LastName  string
	BirthDate *time.Time
	Phone     string
}

// BootstrapSuperuserInput описывает данные первого суперпользователя из конфигурации запуска.
type BootstrapSuperuserInput struct {
	Email     string
	Password  string
	Username  string
	FirstName string
	LastName  string
}

// LoginInput описывает данные входа пользователя.
type LoginInput struct {
	Email    string
	Password string
}

// AuthResult объединяет пользователя и выпущенный JWT.
type AuthResult struct {
	User      *domain.User
	Token     string
	ExpiresAt time.Time
}

// UpdateUserInput описывает частичное обновление профиля пользователя.
type UpdateUserInput struct {
	ID             string
	Username       *string
	FirstName      *string
	LastName       *string
	BirthDate      *time.Time
	ClearBirthDate bool
	Phone          *string
}

// ListUsersInput задаёт поиск и пагинацию списка пользователей.
type ListUsersInput struct {
	Search string
	Limit  int
	Offset int
}

// SetAdminInput описывает изменение роли admin по user ID.
type SetAdminInput struct {
	ActorID string
	UserID  string
	Admin   bool
}

// SetAdminByUsernameInput описывает изменение роли admin по username.
type SetAdminByUsernameInput struct {
	ActorID  string
	Username string
	Admin    bool
}
