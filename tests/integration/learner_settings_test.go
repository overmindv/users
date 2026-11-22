package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/auth"
	graphqldelivery "github.com/overmindv/users/internal/delivery/graphql"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
)

func TestLearnerSettingsGraphQL(t *testing.T) {
	t.Parallel()
	repository := newGraphQLUserRepository()
	jwt := auth.NewManager("test-secret", "users", time.Hour)
	users := usecase.NewUserService(repository, security.PlainTextHasher{}, jwt, &graphQLIDs{values: []string{"admin-id", "student-id"}}, graphQLClock{})
	if err := users.EnsureBootstrapSuperuser(context.Background(), usecase.BootstrapSuperuserInput{Email: "admin@example.com", Password: "password", Username: "superadmin"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	graphqldelivery.Register(mux, &graphqldelivery.Resolver{Users: users}, func(h http.Handler) http.Handler { return auth.OptionalHTTP(jwt, h) }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	owner, id := registerViaGraphQL(t, mux)
	admin := loginViaGraphQL(t, mux, "admin@example.com", "password")
	for _, token := range []string{owner, ""} {
		response := postGraphQL(t, mux, token, `{user(id:"admin-id"){email timezone locale firstName}}`, nil)
		if len(response.Errors) == 0 {
			t.Fatal("non-owner read private profile")
		}
	}
	for _, token := range []string{owner, admin} {
		response := postGraphQL(t, mux, token, `query($id:ID!){user(id:$id){email timezone locale publicName leaderboardVisible}}`, map[string]any{"id": id})
		if len(response.Errors) > 0 {
			t.Fatalf("owner/admin cannot read settings: %v", response.Errors)
		}
	}
	update := `mutation($id:ID!,$input:UpdateUserInput!){updateUser(id:$id,input:$input){timezone locale publicName leaderboardVisible}}`
	response := postGraphQL(t, mux, owner, update, map[string]any{"id": id, "input": map[string]any{"timezone": "Europe/Madrid", "locale": "en-us", "publicName": true, "leaderboardVisible": true}})
	if len(response.Errors) > 0 {
		t.Fatal(response.Errors)
	}
	var data struct {
		UpdateUser struct {
			Timezone, Locale               string
			PublicName, LeaderboardVisible bool
		}
	}
	decodeData(t, response.Data, &data)
	if data.UpdateUser.Timezone != "Europe/Madrid" || data.UpdateUser.Locale != "en-US" || !data.UpdateUser.PublicName || !data.UpdateUser.LeaderboardVisible {
		t.Fatal("settings roundtrip failed")
	}
	response = postGraphQL(t, mux, owner, update, map[string]any{"id": "admin-id", "input": map[string]any{"publicName": true}})
	if len(response.Errors) == 0 {
		t.Fatal("owner changed another account")
	}
	response = postGraphQL(t, mux, owner, update, map[string]any{"id": id, "input": map[string]any{"timezone": "Local"}})
	if len(response.Errors) == 0 {
		t.Fatal("invalid timezone accepted")
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ publicUser(id:\"student-id\") { email timezone } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+owner)
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, req)
	if result.Code != http.StatusUnprocessableEntity || !strings.Contains(result.Body.String(), "GRAPHQL_VALIDATION_FAILED") {
		t.Fatal("public schema exposed private fields")
	}
}
