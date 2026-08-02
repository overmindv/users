package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/overmindv/users/internal/domain"
)

// UserService реализует бизнес-сценарии пользователей, профиля, входа и ролей.
type UserService struct {
	repository UserRepository
	passwords  PasswordHasher
	tokens     TokenManager
	ids        IDGenerator
	clock      Clock
}

// NewUserService собирает user usecase из repository, password service, token manager, ID generator и clock.
func NewUserService(repository UserRepository, passwords PasswordHasher, tokens TokenManager, ids IDGenerator, clock Clock) *UserService {
	return &UserService{repository: repository, passwords: passwords, tokens: tokens, ids: ids, clock: clock}
}

// EnsureBootstrapSuperuser создаёт или повышает первого суперпользователя из конфигурации запуска.
func (s *UserService) EnsureBootstrapSuperuser(ctx context.Context, input BootstrapSuperuserInput) error {
	input.Email = strings.TrimSpace(input.Email)
	input.Username = strings.TrimSpace(input.Username)
	if input.Email == "" || input.Username == "" || input.Password == "" {
		return nil
	}
	if len(input.Password) < 8 {
		return domain.ErrInvalidPassword
	}

	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return err
	}

	existing, err := s.repository.GetByEmail(ctx, email)
	if err == nil {
		if existing.IsSuperuser() {
			return nil
		}
		// Повышаем уже существующего пользователя, чтобы bootstrap был идемпотентным и не создавал дубль по email.
		existing.PromoteSuperuser(s.clock.Now())

		return s.repository.UpdateRoles(ctx, existing)
	}
	if !errors.Is(err, domain.ErrUserNotFound) {
		return fmt.Errorf("get bootstrap superuser: %w", err)
	}

	username, err := domain.NewUsername(input.Username)
	if err != nil {
		return err
	}

	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}

	user, err := domain.NewUser(domain.NewUserParams{
		ID:           s.ids.New(),
		Email:        email,
		PasswordHash: hash,
		Username:     username,
		FirstName:    input.FirstName,
		LastName:     input.LastName,
		Roles:        []string{domain.RoleAdmin},
		IsSuperuser:  true,
		Now:          s.clock.Now(),
	})
	if err != nil {
		return err
	}

	return s.repository.Create(ctx, user)
}

// Register создаёт обычного пользователя и сразу выпускает JWT для входа.
func (s *UserService) Register(ctx context.Context, input RegisterInput) (*AuthResult, error) {
	if len(input.Password) < 8 {
		return nil, domain.ErrInvalidPassword
	}

	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return nil, err
	}

	username, err := domain.NewUsername(input.Username)
	if err != nil {
		return nil, err
	}

	phone, err := domain.NewPhone(input.Phone)
	if err != nil {
		return nil, err
	}

	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user, err := domain.NewUser(domain.NewUserParams{
		ID:           s.ids.New(),
		Email:        email,
		PasswordHash: hash,
		Username:     username,
		FirstName:    input.FirstName,
		LastName:     input.LastName,
		BirthDate:    input.BirthDate,
		Phone:        phone,
		Now:          s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}

	if err := s.repository.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return s.authResult(user)
}

// Login проверяет email и пароль существующего пользователя и возвращает JWT.
// Если пользователь не найден, аккаунт не создаётся и возвращается ошибка входа.
func (s *UserService) Login(ctx context.Context, input LoginInput) (*AuthResult, error) {
	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	user, err := s.repository.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	if err := s.passwords.Compare(user.PasswordHash(), input.Password); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	return s.authResult(user)
}

// Get возвращает активного пользователя по ID.
func (s *UserService) Get(ctx context.Context, id string) (*domain.User, error) {
	if strings.TrimSpace(id) == "" {
		return nil, domain.ErrUserNotFound
	}

	user, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

// GetByUsername возвращает активного пользователя по username.
func (s *UserService) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	value, err := domain.NewUsername(username)
	if err != nil {
		return nil, err
	}

	user, err := s.repository.GetByUsername(ctx, value)
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}

	return user, nil
}

// List возвращает список активных пользователей с поиском и ограниченной пагинацией.
func (s *UserService) List(ctx context.Context, input ListUsersInput) ([]*domain.User, error) {
	limit := input.Limit
	offset := input.Offset
	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	if offset < 0 {
		offset = 0
	}

	users, err := s.repository.List(ctx, input.Search, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	return users, nil
}

// Update применяет частичное обновление профиля пользователя.
func (s *UserService) Update(ctx context.Context, input UpdateUserInput) (*domain.User, error) {
	user, err := s.repository.GetByID(ctx, input.ID)
	if err != nil {
		return nil, fmt.Errorf("get user for update: %w", err)
	}

	patch := domain.ProfilePatch{FirstName: input.FirstName, LastName: input.LastName}
	if input.Username != nil {
		value, err := domain.NewUsername(*input.Username)
		if err != nil {
			return nil, err
		}
		patch.Username = &value
	}

	if input.Phone != nil {
		value, err := domain.NewPhone(*input.Phone)
		if err != nil {
			return nil, err
		}
		patch.Phone = &value
	}

	if input.ClearBirthDate {
		var empty *time.Time
		// Используем указатель на nil, чтобы отличить очистку birth_date от отсутствия поля в update input.
		patch.BirthDate = &empty
	} else if input.BirthDate != nil {
		patch.BirthDate = &input.BirthDate
	}

	if err := user.UpdateProfile(patch, s.clock.Now()); err != nil {
		return nil, err
	}

	if err := s.repository.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}

	return user, nil
}

// SetAdmin меняет роль admin у пользователя после проверки прав actor.
func (s *UserService) SetAdmin(ctx context.Context, input SetAdminInput) (*domain.User, error) {
	if err := s.RequireAdmin(ctx, input.ActorID); err != nil {
		return nil, err
	}

	user, err := s.repository.GetByID(ctx, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user for admin change: %w", err)
	}

	if err := user.SetAdmin(input.Admin, s.clock.Now()); err != nil {
		return nil, err
	}

	if err := s.repository.UpdateRoles(ctx, user); err != nil {
		return nil, fmt.Errorf("update user roles: %w", err)
	}

	return user, nil
}

// SetAdminByUsername меняет роль admin у пользователя, найденного по username.
func (s *UserService) SetAdminByUsername(ctx context.Context, input SetAdminByUsernameInput) (*domain.User, error) {
	username, err := domain.NewUsername(input.Username)
	if err != nil {
		return nil, err
	}

	user, err := s.repository.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("get user by username for admin change: %w", err)
	}

	return s.SetAdmin(ctx, SetAdminInput{
		ActorID: input.ActorID,
		UserID:  user.ID(),
		Admin:   input.Admin,
	})
}

// Delete выполняет soft delete пользователя, запрещая удаление суперпользователя.
func (s *UserService) Delete(ctx context.Context, id string) error {
	user, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get user for delete: %w", err)
	}
	if user.IsSuperuser() {
		return domain.ErrPermissionDenied
	}

	user.SoftDelete(s.clock.Now())
	if err := s.repository.SoftDelete(ctx, user); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	return nil
}

// authResult выпускает JWT и собирает результат успешной регистрации или входа.
func (s *UserService) authResult(user *domain.User) (*AuthResult, error) {
	token, expiresAt, err := s.tokens.Issue(user.ID(), user.Roles())
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}

	return &AuthResult{User: user, Token: token, ExpiresAt: expiresAt}, nil
}

// RequireAdmin проверяет, что actor существует и имеет административные права.
func (s *UserService) RequireAdmin(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return domain.ErrUnauthorized
	}

	user, err := s.repository.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get actor: %w", err)
	}
	if !user.IsAdmin() {
		return domain.ErrPermissionDenied
	}

	return nil
}
