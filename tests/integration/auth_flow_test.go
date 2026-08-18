package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/auth"
	graphqldelivery "github.com/overmindv/users/internal/delivery/graphql"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
)

// graphQLUserRepository имитирует repository для проверки GraphQL transport без PostgreSQL.
type graphQLUserRepository struct {
	users map[string]*domain.User
}

// newGraphQLUserRepository создаёт пустой in-memory repository для GraphQL integration-теста.
func newGraphQLUserRepository() *graphQLUserRepository {
	return &graphQLUserRepository{users: map[string]*domain.User{}}
}

// Create сохраняет пользователя и имитирует уникальность email/username.
func (r *graphQLUserRepository) Create(_ context.Context, user *domain.User) error {
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

// GetByID возвращает активного пользователя по ID из in-memory repository.
func (r *graphQLUserRepository) GetByID(_ context.Context, id string) (*domain.User, error) {
	user, ok := r.users[id]
	if !ok || user.DeletedAt() != nil {
		return nil, domain.ErrUserNotFound
	}

	return user, nil
}

// GetByEmail возвращает активного пользователя по email из in-memory repository.
func (r *graphQLUserRepository) GetByEmail(_ context.Context, email domain.Email) (*domain.User, error) {
	for _, user := range r.users {
		if user.Email() == email && user.DeletedAt() == nil {
			return user, nil
		}
	}

	return nil, domain.ErrUserNotFound
}

// GetByUsername возвращает активного пользователя по username из in-memory repository.
func (r *graphQLUserRepository) GetByUsername(_ context.Context, username domain.Username) (*domain.User, error) {
	for _, user := range r.users {
		if user.Username() == username && user.DeletedAt() == nil {
			return user, nil
		}
	}

	return nil, domain.ErrUserNotFound
}

// List возвращает активных пользователей с простым поиском для GraphQL integration-теста.
func (r *graphQLUserRepository) List(_ context.Context, search string, limit, offset int) ([]*domain.User, error) {
	result := make([]*domain.User, 0, len(r.users))
	search = strings.ToLower(strings.TrimSpace(search))
	for _, user := range r.users {
		if user.DeletedAt() != nil {
			continue
		}
		if search != "" && !strings.Contains(user.Email().String(), search) && !strings.Contains(user.Username().String(), search) {
			continue
		}
		result = append(result, user)
	}
	if offset >= len(result) {
		return []*domain.User{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}

	return result[offset:end], nil
}

// ListPublic использует тот же in-memory поиск в integration-тесте.
func (r *graphQLUserRepository) ListPublic(ctx context.Context, search string, limit, offset int) ([]*domain.User, error) {
	return r.List(ctx, search, limit, offset)
}

// SetAvatar сохраняет avatar state через общий update helper.
func (r *graphQLUserRepository) SetAvatar(ctx context.Context, user *domain.User) error {
	return r.Update(ctx, user)
}

// Update сохраняет изменения пользователя в in-memory repository.
func (r *graphQLUserRepository) Update(_ context.Context, user *domain.User) error {
	if _, ok := r.users[user.ID()]; !ok {
		return domain.ErrUserNotFound
	}
	r.users[user.ID()] = user

	return nil
}

// UpdateRoles сохраняет изменения ролей через общий update helper.
func (r *graphQLUserRepository) UpdateRoles(ctx context.Context, user *domain.User) error {
	return r.Update(ctx, user)
}

// SoftDelete сохраняет soft delete состояние через общий update helper.
func (r *graphQLUserRepository) SoftDelete(ctx context.Context, user *domain.User) error {
	return r.Update(ctx, user)
}

// graphQLIDs возвращает предсказуемые ID для GraphQL auth flow.
type graphQLIDs struct {
	values []string
	index  int
}

// New возвращает следующий ID из prepared sequence.
func (g *graphQLIDs) New() string {
	value := g.values[g.index]
	g.index++

	return value
}

// graphQLClock фиксирует время для стабильных GraphQL assertions.
type graphQLClock struct{}

// Now возвращает фиксированное UTC-время.
func (graphQLClock) Now() time.Time {
	return time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
}

// graphQLHealth имитирует успешный healthcheck для GraphQL handler.
type graphQLHealth struct{}

// Ping возвращает успешный результат healthcheck.
func (graphQLHealth) Ping(context.Context) error {
	return nil
}

// graphQLResponse описывает минимальную GraphQL response shape для assertions.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

// TestGraphQLAuthFlow проверяет регистрацию, вход, update и admin promotion через GraphQL handler Users.
func TestGraphQLAuthFlow(t *testing.T) {
	repository := newGraphQLUserRepository()
	jwt := auth.NewManager("test-secret", "users", time.Hour)
	users := usecase.NewUserService(
		repository,
		security.PlainTextHasher{},
		jwt,
		&graphQLIDs{values: []string{"admin-id", "student-id"}},
		graphQLClock{},
	)

	if err := users.EnsureBootstrapSuperuser(context.Background(), usecase.BootstrapSuperuserInput{
		Email:    "admin@example.com",
		Password: "password",
		Username: "superadmin",
	}); err != nil {
		t.Fatal(err)
	}

	handler := auth.OptionalHTTP(jwt, graphqldelivery.Handler(
		&graphqldelivery.Resolver{Users: users},
		graphQLHealth{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	))
	studentToken, studentID := registerViaGraphQL(t, handler)
	loginToken := loginViaGraphQL(t, handler, "student@example.com", "password")
	if loginToken == "" || studentToken == "" {
		t.Fatal("expected register and login tokens")
	}

	updateUserViaGraphQL(t, handler, studentToken, studentID)
	assertLoginFails(t, handler, "missing@example.com", "password")
	regularCannotPromote(t, handler, studentToken, "superadmin")

	adminToken := loginViaGraphQL(t, handler, "admin@example.com", "password")
	promoted := setAdminViaGraphQL(t, handler, adminToken, "student", true)
	if !promoted.IsAdmin || promoted.IsSuperuser {
		t.Fatalf("expected promoted regular admin, got %+v", promoted)
	}
}

// registerViaGraphQL выполняет GraphQL mutation регистрации и возвращает token и user ID.
func registerViaGraphQL(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()

	response := postGraphQL(t, handler, "", `mutation Register($input: RegisterInput!) {
		register(input: $input) { token user { id email username roles isAdmin isSuperuser } }
	}`, map[string]any{
		"input": map[string]any{
			"email":     "student@example.com",
			"password":  "password",
			"username":  "student",
			"firstName": "Student",
		},
	})
	if len(response.Errors) > 0 {
		t.Fatalf("register errors: %+v", response.Errors)
	}

	var data struct {
		Register struct {
			Token string `json:"token"`
			User  struct {
				ID          string   `json:"id"`
				Email       string   `json:"email"`
				Username    string   `json:"username"`
				Roles       []string `json:"roles"`
				IsAdmin     bool     `json:"isAdmin"`
				IsSuperuser bool     `json:"isSuperuser"`
			} `json:"user"`
		} `json:"register"`
	}
	decodeData(t, response.Data, &data)
	if data.Register.User.ID != "student-id" || data.Register.User.IsAdmin || len(data.Register.User.Roles) != 0 {
		t.Fatalf("unexpected registered user: %+v", data.Register.User)
	}

	return data.Register.Token, data.Register.User.ID
}

// loginViaGraphQL выполняет GraphQL mutation входа и возвращает JWT.
func loginViaGraphQL(t *testing.T, handler http.Handler, email, password string) string {
	t.Helper()

	response := postGraphQL(t, handler, "", `mutation Login($input: LoginInput!) {
		login(input: $input) { token user { id email username isAdmin } }
	}`, map[string]any{
		"input": map[string]any{
			"email":    email,
			"password": password,
		},
	})
	if len(response.Errors) > 0 {
		t.Fatalf("login errors: %+v", response.Errors)
	}

	var data struct {
		Login struct {
			Token string `json:"token"`
		} `json:"login"`
	}
	decodeData(t, response.Data, &data)

	return data.Login.Token
}

// updateUserViaGraphQL выполняет GraphQL mutation обновления профиля текущего пользователя.
func updateUserViaGraphQL(t *testing.T, handler http.Handler, token, id string) {
	t.Helper()

	response := postGraphQL(t, handler, token, `mutation UpdateUser($id: ID!, $input: UpdateUserInput!) {
		updateUser(id: $id, input: $input) { id firstName username }
	}`, map[string]any{
		"id": id,
		"input": map[string]any{
			"firstName": "Updated",
		},
	})
	if len(response.Errors) > 0 {
		t.Fatalf("update errors: %+v", response.Errors)
	}

	var data struct {
		UpdateUser struct {
			FirstName string `json:"firstName"`
		} `json:"updateUser"`
	}
	decodeData(t, response.Data, &data)
	if data.UpdateUser.FirstName != "Updated" {
		t.Fatalf("profile was not updated: %+v", data.UpdateUser)
	}
}

// assertLoginFails проверяет, что неизвестный пользователь не создаётся через login.
func assertLoginFails(t *testing.T, handler http.Handler, email, password string) {
	t.Helper()

	response := postGraphQL(t, handler, "", `mutation Login($input: LoginInput!) {
		login(input: $input) { token }
	}`, map[string]any{
		"input": map[string]any{
			"email":    email,
			"password": password,
		},
	})
	if len(response.Errors) == 0 {
		t.Fatal("expected unknown login to fail")
	}

	code, _ := response.Errors[0].Extensions["code"].(string)
	if code != "UNAUTHENTICATED" {
		t.Fatalf("unexpected error code: %q", code)
	}
}

// graphQLUser описывает поля пользователя, нужные для проверки admin mutation.
type graphQLUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	IsAdmin     bool   `json:"isAdmin"`
	IsSuperuser bool   `json:"isSuperuser"`
}

// setAdminViaGraphQL выполняет GraphQL mutation назначения или снятия admin.
func setAdminViaGraphQL(t *testing.T, handler http.Handler, token, username string, admin bool) graphQLUser {
	t.Helper()

	response := postGraphQL(t, handler, token, `mutation SetUserAdminByUsername($username: String!, $admin: Boolean!) {
		setUserAdminByUsername(username: $username, admin: $admin) { id username isAdmin isSuperuser }
	}`, map[string]any{
		"username": username,
		"admin":    admin,
	})
	if len(response.Errors) > 0 {
		t.Fatalf("set admin errors: %+v", response.Errors)
	}

	var data struct {
		SetUserAdminByUsername graphQLUser `json:"setUserAdminByUsername"`
	}
	decodeData(t, response.Data, &data)

	return data.SetUserAdminByUsername
}

// regularCannotPromote проверяет, что обычный пользователь не может назначать администраторов.
func regularCannotPromote(t *testing.T, handler http.Handler, token, username string) {
	t.Helper()

	response := postGraphQL(t, handler, token, `mutation SetUserAdminByUsername($username: String!, $admin: Boolean!) {
		setUserAdminByUsername(username: $username, admin: $admin) { id }
	}`, map[string]any{
		"username": username,
		"admin":    true,
	})
	if len(response.Errors) == 0 {
		t.Fatal("expected regular user admin change to fail")
	}

	code, _ := response.Errors[0].Extensions["code"].(string)
	if code != "FORBIDDEN" {
		t.Fatalf("unexpected error code: %q", code)
	}
}

// postGraphQL отправляет request в GraphQL handler без запуска сетевого listener.
func postGraphQL(t *testing.T, handler http.Handler, token, query string, variables map[string]any) graphQLResponse {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected HTTP status %d: %s", recorder.Code, recorder.Body.String())
	}

	var response graphQLResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	return response
}

// decodeData декодирует GraphQL data payload в target-структуру теста.
func decodeData(t *testing.T, data json.RawMessage, target any) {
	t.Helper()

	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
