package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/overmindv/arcee/internal/domain"
	"github.com/overmindv/arcee/internal/security"
)

type fakeRepository struct {
	users     map[string]*domain.User
	err       error
	updateErr error
	deleteErr error
}

func newFakeRepository() *fakeRepository { return &fakeRepository{users: map[string]*domain.User{}} }

func (r *fakeRepository) Create(_ context.Context, user *domain.User) error {
	if r.err != nil {
		return r.err
	}

	for _, existing := range r.users {
		if existing.Email() == user.Email() {
			return domain.ErrEmailAlreadyExists
		}
		if existing.Username() == user.Username() {
			return domain.ErrUsernameExists
		}
	}

	r.users[user.ID()] = user

	return nil
}
func (r *fakeRepository) GetByID(_ context.Context, id string) (*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}

	user, ok := r.users[id]
	if !ok || user.DeletedAt() != nil {
		return nil, domain.ErrUserNotFound
	}

	return user, nil
}
func (r *fakeRepository) GetByEmail(_ context.Context, email domain.Email) (*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}

	for _, user := range r.users {
		if user.Email() == email && user.DeletedAt() == nil {
			return user, nil
		}
	}

	return nil, domain.ErrUserNotFound
}
func (r *fakeRepository) List(context.Context, int, int) ([]*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}

	result := make([]*domain.User, 0, len(r.users))
	for _, user := range r.users {
		if user.DeletedAt() == nil {
			result = append(result, user)
		}
	}

	return result, nil
}
func (r *fakeRepository) Update(_ context.Context, user *domain.User) error {
	if r.updateErr != nil {
		return r.updateErr
	}

	if r.err != nil {
		return r.err
	}

	r.users[user.ID()] = user

	return nil
}
func (r *fakeRepository) SoftDelete(_ context.Context, user *domain.User) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}

	if r.err != nil {
		return r.err
	}

	r.users[user.ID()] = user

	return nil
}

type fakeIDs struct{}

func (fakeIDs) New() string { return "user-id" }

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

type fakeTokens struct{ err error }

func (t fakeTokens) Issue(id string) (string, time.Time, error) {
	return "token-" + id, time.Unix(100, 0), t.err
}
func (fakeTokens) Parse(token string) (string, error) { return token, nil }

type brokenHasher struct{}

func (brokenHasher) Hash(string) (string, error)  { return "", errors.New("hash failed") }
func (brokenHasher) Compare(string, string) error { return errors.New("compare failed") }

func newService(repository *fakeRepository) *UserService {
	return NewUserService(repository, security.PlainTextHasher{}, fakeTokens{}, fakeIDs{}, fakeClock{time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)})
}

func TestRegisterLoginUpdateDelete(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)
	ctx := context.Background()

	registered, err := service.Register(ctx, RegisterInput{Email: "Alice@example.com", Password: "password", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if registered.Token != "token-user-id" || registered.User.Email().String() != "alice@example.com" {
		t.Fatalf("unexpected registration: %+v", registered)
	}

	loggedIn, err := service.Login(ctx, LoginInput{Email: "alice@example.com", Password: "password"})
	if err != nil || loggedIn.User.ID() != "user-id" {
		t.Fatalf("Login() = %+v, %v", loggedIn, err)
	}

	name, username := "Alice", "alice_new"
	updated, err := service.Update(ctx, UpdateUserInput{ID: "user-id", FirstName: &name, Username: &username})

	if err != nil || updated.FirstName() != name || updated.Username().String() != username {
		t.Fatalf("Update() = %+v, %v", updated, err)
	}

	if err := service.Delete(ctx, "user-id"); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Get(ctx, "user-id"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestValidationAndConflicts(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)
	ctx := context.Background()

	if _, err := service.Register(ctx, RegisterInput{Email: "bad", Password: "password", Username: "alice"}); !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("expected invalid email, got %v", err)
	}

	if _, err := service.Register(ctx, RegisterInput{Email: "a@example.com", Password: "short", Username: "alice"}); !errors.Is(err, domain.ErrInvalidPassword) {
		t.Fatalf("expected invalid password, got %v", err)
	}

	input := RegisterInput{Email: "a@example.com", Password: "password", Username: "alice"}
	if _, err := service.Register(ctx, input); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Register(ctx, input); !errors.Is(err, domain.ErrEmailAlreadyExists) {
		t.Fatalf("expected conflict, got %v", err)
	}

	second := RegisterInput{Email: "b@example.com", Password: "password", Username: "alice"}
	if _, err := service.Register(ctx, second); !errors.Is(err, domain.ErrUsernameExists) {
		t.Fatalf("expected username conflict, got %v", err)
	}

	if _, err := service.Login(ctx, LoginInput{Email: input.Email, Password: "incorrect"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}

	if _, err := service.Login(ctx, LoginInput{Email: "none@example.com", Password: "password"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestUpdateValidationAndClearBirthDate(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)
	ctx := context.Background()
	birthDate := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := service.Register(ctx, RegisterInput{Email: "a@example.com", Password: "password", Username: "alice", BirthDate: &birthDate}); err != nil {
		t.Fatal(err)
	}

	badUsername := "!"
	if _, err := service.Update(ctx, UpdateUserInput{ID: "user-id", Username: &badUsername}); !errors.Is(err, domain.ErrInvalidUsername) {
		t.Fatalf("expected invalid username, got %v", err)
	}

	badPhone := "8999"
	if _, err := service.Update(ctx, UpdateUserInput{ID: "user-id", Phone: &badPhone}); !errors.Is(err, domain.ErrInvalidPhone) {
		t.Fatalf("expected invalid phone, got %v", err)
	}

	updated, err := service.Update(ctx, UpdateUserInput{ID: "user-id", ClearBirthDate: true})
	if err != nil || updated.BirthDate() != nil {
		t.Fatalf("clear birth date = %+v, %v", updated, err)
	}

	repository.updateErr = errors.New("write failed")
	name := "Updated"
	if _, err := service.Update(ctx, UpdateUserInput{ID: "user-id", FirstName: &name}); err == nil {
		t.Fatal("expected update error")
	}
}

func TestDependencyErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newFakeRepository()
	clock := fakeClock{time.Now()}

	service := NewUserService(repository, brokenHasher{}, fakeTokens{}, fakeIDs{}, clock)
	if _, err := service.Register(ctx, RegisterInput{Email: "a@example.com", Password: "password", Username: "alice"}); err == nil {
		t.Fatal("expected hash error")
	}

	service = NewUserService(repository, security.PlainTextHasher{}, fakeTokens{err: errors.New("sign failed")}, fakeIDs{}, clock)
	if _, err := service.Register(ctx, RegisterInput{Email: "a@example.com", Password: "password", Username: "alice"}); err == nil {
		t.Fatal("expected token error")
	}

	service = newService(repository)
	if _, err := service.Get(ctx, ""); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	repository.err = errors.New("read failed")
	if _, err := service.Get(ctx, "user-id"); err == nil {
		t.Fatal("expected read error")
	}

	if _, err := service.Login(ctx, LoginInput{Email: "a@example.com", Password: "password"}); err == nil {
		t.Fatal("expected login repository error")
	}

	repository.err = nil
	if _, err := service.Register(ctx, RegisterInput{Email: "c@example.com", Password: "password", Username: "charlie"}); err != nil {
		t.Fatal(err)
	}

	repository.deleteErr = errors.New("delete failed")
	if err := service.Delete(ctx, "user-id"); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestListBoundsAndRepositoryErrors(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)

	if users, err := service.List(context.Background(), -1, -1); err != nil || len(users) != 0 {
		t.Fatalf("List() = %v, %v", users, err)
	}

	repository.err = errors.New("database unavailable")
	if _, err := service.List(context.Background(), 200, 0); err == nil {
		t.Fatal("expected repository error")
	}
}
