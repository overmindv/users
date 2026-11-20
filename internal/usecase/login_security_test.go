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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

type loginThrottleStub struct {
	allowed            bool
	checkErr, resetErr error
	email, ip          string
}

func (s *loginThrottleStub) Check(_ context.Context, email, ip string) (bool, error) {
	s.email, s.ip = email, ip

	return s.allowed, s.checkErr
}
func (s *loginThrottleStub) ResetEmail(context.Context, string) error { return s.resetErr }

func TestLoginThrottleFailsClosedAndUsesNormalizedIdentity(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	service := NewUserService(repo, security.PlainTextHasher{}, fakeTokens{}, fakeIDs{}, fakeClock{time.Now()})
	if _, err := service.Register(context.Background(), RegisterInput{Email: "user@example.com", Password: "password", Username: "user"}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("storage unavailable")
	throttle := &loginThrottleStub{allowed: true, checkErr: failure}
	service.SetLoginThrottler(throttle)
	input := LoginInput{Email: " USER@EXAMPLE.COM ", Password: "password", ClientIP: "192.0.2.1"}
	if result, err := service.Login(context.Background(), input); result != nil || !errors.Is(err, failure) {
		t.Fatalf("check bypass: %v %v", result, err)
	}
	if throttle.email != "user@example.com" || throttle.ip != input.ClientIP {
		t.Fatal("identity not normalized/passed")
	}
	throttle.checkErr, throttle.resetErr = nil, failure
	if result, err := service.Login(context.Background(), input); result != nil || !errors.Is(err, failure) {
		t.Fatalf("reset bypass: %v %v", result, err)
	}
}

func TestUnknownAccountHasSameLoginErrorsAndLimit(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	service := NewUserService(repo, security.PlainTextHasher{}, fakeTokens{}, fakeIDs{}, fakeClock{time.Now()})
	if _, err := service.Register(context.Background(), RegisterInput{Email: "known@example.com", Password: "password", Username: "user"}); err != nil {
		t.Fatal(err)
	}
	service.SetLoginThrottler(security.NewMemoryLoginThrottler(2, time.Minute))
	for i := 0; i < 3; i++ {
		for _, email := range []string{"known@example.com", "unknown@example.com"} {
			_, err := service.Login(context.Background(), LoginInput{Email: email, Password: "wrong"})
			want := domain.ErrInvalidCredentials
			if i == 2 {
				want = domain.ErrTooManyRequests
			}
			if !errors.Is(err, want) {
				t.Fatalf("attempt %d: %v", i, err)
			}
		}
	}
}

func TestNonLocalRejectsDemoAndLegacyCredentials(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"00000000-0000-0000-0000-000000000001", "regular-user"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepository()
			email, _ := domain.NewEmail("demo@example.com")
			username, _ := domain.NewUsername("demo_user")
			hash := "password"
			if strings.HasPrefix(id, "00000000") {
				var err error
				hash, err = security.NewArgon2IDHasher().Hash("password")
				if err != nil {
					t.Fatal(err)
				}
			}
			user, _ := domain.NewUser(domain.NewUserParams{ID: id, Email: email, Username: username, PasswordHash: hash, Now: time.Now()})
			repo.users[id] = user
			service := NewUserService(repo, security.NewArgon2IDHasher(), fakeTokens{}, fakeIDs{}, fakeClock{time.Now()})
			service.SetLocalCredentials(false)
			if _, err := service.Login(context.Background(), LoginInput{Email: "demo@example.com", Password: "password"}); !errors.Is(err, domain.ErrInvalidCredentials) {
				t.Fatalf("unsafe account accepted: %v", err)
			}
		})
	}
}
