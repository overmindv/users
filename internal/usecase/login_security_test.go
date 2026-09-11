package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
)

// TestLoginLazyMigratesLegacyPasswordHash проверяет, что устаревший (открытый) hash пароля
// при успешном входе пере-хэшируется Argon2id и сохраняется.
func TestLoginLazyMigratesLegacyPasswordHash(t *testing.T) {
	repository := newFakeRepository()
	registerClock := fakeClock{time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}

	email, err := domain.NewEmail("migrate@example.com")
	if err != nil {
		t.Fatal(err)
	}
	username, err := domain.NewUsername("migrate_user")
	if err != nil {
		t.Fatal(err)
	}
	// Legacy-запись с паролем в открытом виде (как было до внедрения Argon2id).
	legacy, err := domain.NewUser(domain.NewUserParams{
		ID:           "user-id",
		Email:        email,
		PasswordHash: "secret-pass",
		Username:     username,
		Now:          registerClock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	repository.users[legacy.ID()] = legacy

	service := NewUserService(
		repository,
		security.NewArgon2IDHasher(),
		fakeTokens{},
		fakeIDs{},
		fakeClock{time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)},
	)

	if _, err := service.Login(context.Background(), LoginInput{Email: "migrate@example.com", Password: "secret-pass"}); err != nil {
		t.Fatalf("expected legacy login to succeed: %v", err)
	}

	stored := repository.users[legacy.ID()].PasswordHash()
	if !strings.HasPrefix(stored, "$argon2id$") {
		t.Fatalf("expected argon2id hash after lazy migration, got %q", stored)
	}
}

// TestLoginLegacyWrongPasswordNotMigrated проверяет, что при неверном пароле
// устаревший hash не перезаписывается.
func TestLoginLegacyWrongPasswordNotMigrated(t *testing.T) {
	repository := newFakeRepository()
	email, _ := domain.NewEmail("migrate@example.com")
	username, _ := domain.NewUsername("migrate_user")
	legacy, err := domain.NewUser(domain.NewUserParams{
		ID:           "user-id",
		Email:        email,
		PasswordHash: "secret-pass",
		Username:     username,
		Now:          time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	repository.users[legacy.ID()] = legacy

	service := NewUserService(repository, security.NewArgon2IDHasher(), fakeTokens{}, fakeIDs{}, fakeClock{time.Now().UTC()})
	if _, err := service.Login(context.Background(), LoginInput{Email: "migrate@example.com", Password: "wrong"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	if repository.users[legacy.ID()].PasswordHash() != "secret-pass" {
		t.Fatal("legacy hash must not be overwritten on failed login")
	}
}

// TestLoginEnforcesRateLimit проверяет, что после исчерпания попыток вход блокируется.
func TestLoginEnforcesRateLimit(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	service := NewUserService(repository, security.NewArgon2IDHasher(), fakeTokens{}, fakeIDs{}, fakeClock{now.Add(-time.Hour)})
	if _, err := service.Register(context.Background(), RegisterInput{Email: "limited@example.com", Password: "password", Username: "limited"}); err != nil {
		t.Fatal(err)
	}
	service.SetLoginThrottler(security.NewMemoryLoginThrottler(3, time.Minute))

	for i := 0; i < 3; i++ {
		if _, err := service.Login(context.Background(), LoginInput{Email: "limited@example.com", Password: "wrong"}); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: expected invalid credentials, got %v", i, err)
		}
	}

	// Даже корректный пароль не пропустится после исчерпания лимита.
	if _, err := service.Login(context.Background(), LoginInput{Email: "limited@example.com", Password: "password"}); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("expected too many requests, got %v", err)
	}
}

// TestLoginResetsCounterOnSuccess проверяет, что успешный вход сбрасывает счётчик попыток.
func TestLoginResetsCounterOnSuccess(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	service := NewUserService(repository, security.NewArgon2IDHasher(), fakeTokens{}, fakeIDs{}, fakeClock{now.Add(-time.Hour)})
	if _, err := service.Register(context.Background(), RegisterInput{Email: "reset@example.com", Password: "password", Username: "reset"}); err != nil {
		t.Fatal(err)
	}
	service.SetLoginThrottler(security.NewMemoryLoginThrottler(2, time.Minute))

	// Один неудачный вход, затем успешный — счётчик сбрасывается.
	if _, err := service.Login(context.Background(), LoginInput{Email: "reset@example.com", Password: "wrong"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	if _, err := service.Login(context.Background(), LoginInput{Email: "reset@example.com", Password: "password"}); err != nil {
		t.Fatalf("successful login after one failure: %v", err)
	}

	// Снова исчерпываем лимит и убеждаемся, что блок сработал.
	for i := 0; i < 2; i++ {
		if _, err := service.Login(context.Background(), LoginInput{Email: "reset@example.com", Password: "wrong"}); err == nil {
			t.Fatal("expected failed login")
		}
	}
	if _, err := service.Login(context.Background(), LoginInput{Email: "reset@example.com", Password: "password"}); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("expected too many requests after renewed failures, got %v", err)
	}
}
