package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
)

// fakeRepository хранит пользователей в памяти для unit-тестов UserService.
type fakeRepository struct {
	users     map[string]*domain.User
	err       error
	updateErr error
	deleteErr error
}

// newFakeRepository создаёт пустой in-memory repository для тестов.
func newFakeRepository() *fakeRepository { return &fakeRepository{users: map[string]*domain.User{}} }

// Create сохраняет пользователя и имитирует уникальность email/username.
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

// GetByID возвращает активного пользователя по ID из test repository.
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

// GetByEmail возвращает активного пользователя по email из test repository.
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

// GetByUsername возвращает активного пользователя по username из test repository.
func (r *fakeRepository) GetByUsername(_ context.Context, username domain.Username) (*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}

	for _, user := range r.users {
		if user.Username() == username && user.DeletedAt() == nil {
			return user, nil
		}
	}

	return nil, domain.ErrUserNotFound
}

// List возвращает активных пользователей и имитирует простой поиск по email/username.
func (r *fakeRepository) List(_ context.Context, search string, _, _ int) ([]*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}

	result := make([]*domain.User, 0, len(r.users))
	for _, user := range r.users {
		if user.DeletedAt() == nil && (search == "" || user.Username().String() == search || user.Email().String() == search) {
			result = append(result, user)
		}
	}

	return result, nil
}

func (r *fakeRepository) ListPublic(ctx context.Context, search string, limit, offset int) ([]*domain.User, error) {
	return r.List(ctx, search, limit, offset)
}

func (r *fakeRepository) SetAvatar(_ context.Context, user *domain.User) error {
	return r.Update(context.Background(), user)
}

// Update сохраняет изменения профиля в test repository.
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

// UpdateRoles сохраняет изменения ролей в test repository.
func (r *fakeRepository) UpdateRoles(_ context.Context, user *domain.User) error {
	if r.updateErr != nil {
		return r.updateErr
	}

	if r.err != nil {
		return r.err
	}

	r.users[user.ID()] = user

	return nil
}

// SoftDelete сохраняет soft delete состояние в test repository.
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

// fakeIDs всегда возвращает один ID для коротких unit-сценариев.
type fakeIDs struct{}

// New возвращает фиксированный test user ID.
func (fakeIDs) New() string { return "user-id" }

// sequenceIDs возвращает ID по очереди для сценариев с несколькими пользователями.
type sequenceIDs struct {
	values []string
	index  int
}

// New возвращает следующий ID из sequenceIDs.
func (g *sequenceIDs) New() string {
	if g.index >= len(g.values) {
		return "user-id-extra"
	}

	value := g.values[g.index]
	g.index++

	return value
}

// fakeClock возвращает фиксированное время для стабильных тестов.
type fakeClock struct{ now time.Time }

// Now возвращает фиксированное UTC-время из fakeClock.
func (c fakeClock) Now() time.Time { return c.now }

// fakeTokens выпускает предсказуемые token values для unit-тестов.
type fakeTokens struct{ err error }

// Issue возвращает test token или заданную ошибку.
func (t fakeTokens) Issue(id string, _ []string) (string, time.Time, error) {
	return "token-" + id, time.Unix(100, 0), t.err
}

// Parse возвращает token как user ID для test contract.
func (fakeTokens) Parse(token string) (string, error) { return token, nil }

type fakeMedia struct {
	userID string
	fileID string
	err    error
}

// ValidateAvatar фиксирует проверяемые IDs и возвращает заданную ошибку.
func (m *fakeMedia) ValidateAvatar(_ context.Context, userID, fileID string) error {
	m.userID = userID
	m.fileID = fileID

	return m.err
}

// brokenHasher имитирует ошибки password hashing и compare.
type brokenHasher struct{}

// Hash возвращает ошибку hashing для negative-тестов.
func (brokenHasher) Hash(string) (string, error) { return "", errors.New("hash failed") }

// Compare возвращает ошибку compare для negative-тестов.
func (brokenHasher) Compare(string, string) error { return errors.New("compare failed") }

// newService собирает UserService с test dependencies.
func newService(repository *fakeRepository) *UserService {
	return NewUserService(repository, security.PlainTextHasher{}, fakeTokens{}, fakeIDs{}, fakeClock{time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)})
}

// TestSetAvatarValidatesAndClears проверяет валидацию Media и очистку без внешнего вызова.
func TestSetAvatarValidatesAndClears(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	media := &fakeMedia{}
	service := NewUserServiceWithMedia(repository, security.PlainTextHasher{}, fakeTokens{}, fakeIDs{}, fakeClock{time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)}, media)
	registered, err := service.Register(context.Background(), RegisterInput{Email: "avatar@example.com", Password: "password", Username: "avatar_user"})
	if err != nil {
		t.Fatalf("зарегистрировать пользователя: %v", err)
	}
	fileID := "11111111-1111-1111-1111-111111111111"
	updated, err := service.SetAvatar(context.Background(), SetAvatarInput{UserID: registered.User.ID(), FileID: &fileID})
	if err != nil {
		t.Fatalf("установить аватар: %v", err)
	}
	if updated.AvatarFileID() == nil || *updated.AvatarFileID() != fileID || media.fileID != fileID {
		t.Fatalf("аватар или media validation не сохранены: user=%+v media=%+v", updated, media)
	}
	media.fileID = ""
	updated, err = service.SetAvatar(context.Background(), SetAvatarInput{UserID: registered.User.ID()})
	if err != nil {
		t.Fatalf("очистить аватар: %v", err)
	}
	if updated.AvatarFileID() != nil || media.fileID != "" {
		t.Fatal("очистка аватара не должна обращаться в Media")
	}
}

// TestRegisterLoginUpdateDelete проверяет основной пользовательский lifecycle без БД.
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

// TestValidationAndConflicts проверяет валидацию регистрации и конфликты уникальности.
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
		t.Fatalf("expected unknown user login to fail, got %v", err)
	}

	if len(repository.users) != 1 {
		t.Fatalf("login must not create users, got %d users", len(repository.users))
	}
}

// TestUpdateValidationAndClearBirthDate проверяет validation update и явную очистку birth_date.
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

// TestDependencyErrors проверяет, что ошибки зависимостей не проглатываются usecase-слоем.
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

// TestListBoundsAndRepositoryErrors проверяет нормализацию pagination и ошибки repository при списке пользователей.
func TestListBoundsAndRepositoryErrors(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)

	if users, err := service.List(context.Background(), ListUsersInput{Limit: -1, Offset: -1}); err != nil || len(users) != 0 {
		t.Fatalf("List() = %v, %v", users, err)
	}

	repository.err = errors.New("database unavailable")
	if _, err := service.List(context.Background(), ListUsersInput{Limit: 200}); err == nil {
		t.Fatal("expected repository error")
	}
}

// TestBootstrapSuperuserCannotBeDemoted проверяет защиту bootstrap-суперпользователя от демоушена.
func TestBootstrapSuperuserCannotBeDemoted(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)
	ctx := context.Background()

	err := service.EnsureBootstrapSuperuser(ctx, BootstrapSuperuserInput{
		Email:    "admin@example.com",
		Password: "password",
		Username: "superadmin",
	})
	if err != nil {
		t.Fatal(err)
	}

	user, err := service.Get(ctx, "user-id")
	if err != nil {
		t.Fatal(err)
	}
	if !user.IsSuperuser() || !user.IsAdmin() {
		t.Fatalf("expected superuser admin, got roles=%v superuser=%v", user.Roles(), user.IsSuperuser())
	}

	_, err = service.SetAdmin(ctx, SetAdminInput{
		ActorID: "user-id",
		UserID:  "user-id",
		Admin:   false,
	})
	if !errors.Is(err, domain.ErrCannotDemoteSuperuser) {
		t.Fatalf("expected cannot demote superuser, got %v", err)
	}
}

// TestAdminCanPromoteRegularUser проверяет назначение и снятие admin обычному пользователю.
func TestAdminCanPromoteRegularUser(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	ids := &sequenceIDs{values: []string{"admin-id", "student-id"}}
	service := NewUserService(repository, security.PlainTextHasher{}, fakeTokens{}, ids, fakeClock{time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)})
	ctx := context.Background()

	if err := service.EnsureBootstrapSuperuser(ctx, BootstrapSuperuserInput{
		Email:    "admin@example.com",
		Password: "password",
		Username: "superadmin",
	}); err != nil {
		t.Fatal(err)
	}

	student, err := service.Register(ctx, RegisterInput{
		Email:    "student@example.com",
		Password: "password",
		Username: "student",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.SetAdmin(ctx, SetAdminInput{
		ActorID: student.User.ID(),
		UserID:  "admin-id",
		Admin:   false,
	}); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("expected regular user to be denied, got %v", err)
	}

	promoted, err := service.SetAdminByUsername(ctx, SetAdminByUsernameInput{
		ActorID:  "admin-id",
		Username: "student",
		Admin:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.IsAdmin() || promoted.IsSuperuser() {
		t.Fatalf("expected promoted regular admin, got roles=%v superuser=%v", promoted.Roles(), promoted.IsSuperuser())
	}

	demoted, err := service.SetAdmin(ctx, SetAdminInput{
		ActorID: "admin-id",
		UserID:  "student-id",
		Admin:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if demoted.IsAdmin() {
		t.Fatalf("expected regular admin role to be removed, got roles=%v", demoted.Roles())
	}
}

// TestBootstrapPromotesExistingUserAndValidatesInput проверяет идемпотентный bootstrap существующего пользователя.
func TestBootstrapPromotesExistingUserAndValidatesInput(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)
	ctx := context.Background()

	if _, err := service.Register(ctx, RegisterInput{
		Email:    "regular@example.com",
		Password: "password",
		Username: "regular",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.EnsureBootstrapSuperuser(ctx, BootstrapSuperuserInput{
		Email:    "regular@example.com",
		Password: "password",
		Username: "regular",
	}); err != nil {
		t.Fatal(err)
	}

	user, err := service.GetByUsername(ctx, "regular")
	if err != nil {
		t.Fatal(err)
	}
	if !user.IsSuperuser() || !user.IsAdmin() {
		t.Fatalf("expected existing user to remain superuser, got roles=%v", user.Roles())
	}

	if err := service.EnsureBootstrapSuperuser(ctx, BootstrapSuperuserInput{
		Email:    "invalid@example.com",
		Password: "short",
		Username: "invalid",
	}); !errors.Is(err, domain.ErrInvalidPassword) {
		t.Fatalf("expected invalid bootstrap password, got %v", err)
	}

	if err := service.EnsureBootstrapSuperuser(ctx, BootstrapSuperuserInput{}); err != nil {
		t.Fatalf("empty bootstrap config must be ignored, got %v", err)
	}
}

// TestAdminLookupErrors проверяет ошибки поиска и проверки admin actor.
func TestAdminLookupErrors(t *testing.T) {
	t.Parallel()

	repository := newFakeRepository()
	service := newService(repository)
	ctx := context.Background()

	if _, err := service.GetByUsername(ctx, "!"); !errors.Is(err, domain.ErrInvalidUsername) {
		t.Fatalf("expected invalid username, got %v", err)
	}

	if err := service.RequireAdmin(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	if err := service.RequireAdmin(ctx, "missing"); err == nil {
		t.Fatal("expected missing actor error")
	}

	if _, err := service.SetAdminByUsername(ctx, SetAdminByUsernameInput{
		ActorID:  "missing",
		Username: "unknown",
		Admin:    true,
	}); err == nil {
		t.Fatal("expected missing target error")
	}
}
