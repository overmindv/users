package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/overmindv/arcee/internal/domain"
)

type UserService struct {
	repository UserRepository
	passwords  PasswordHasher
	tokens     TokenManager
	ids        IDGenerator
	clock      Clock
}

func NewUserService(repository UserRepository, passwords PasswordHasher, tokens TokenManager, ids IDGenerator, clock Clock) *UserService {
	return &UserService{repository: repository, passwords: passwords, tokens: tokens, ids: ids, clock: clock}
}

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
		ID: s.ids.New(), Email: email, PasswordHash: hash, Username: username,
		FirstName: input.FirstName, LastName: input.LastName, BirthDate: input.BirthDate,
		Phone: phone, Now: s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}

	if err := s.repository.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return s.authResult(user)
}

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

func (s *UserService) List(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	if offset < 0 {
		offset = 0
	}

	users, err := s.repository.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	return users, nil
}

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

func (s *UserService) Delete(ctx context.Context, id string) error {
	user, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get user for delete: %w", err)
	}

	user.SoftDelete(s.clock.Now())
	if err := s.repository.SoftDelete(ctx, user); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	return nil
}

func (s *UserService) authResult(user *domain.User) (*AuthResult, error) {
	token, expiresAt, err := s.tokens.Issue(user.ID())
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}

	return &AuthResult{User: user, Token: token, ExpiresAt: expiresAt}, nil
}
