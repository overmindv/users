//go:build component

package integration

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/overmindv/users/internal/auth"
	graphqldelivery "github.com/overmindv/users/internal/delivery/graphql"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

// TestRefreshGraphQL проверяет opt-in и новый контракт через настоящий PostgreSQL.
func TestRefreshGraphQL(t *testing.T) {
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	manager := auth.NewManager("refresh-test-secret", "users", time.Hour)
	manager.SetSessionStore(repo)
	manager.SetRefreshStore(repo)
	svc := usecase.NewUserService(repo, security.PlainTextHasher{}, manager, refreshIDs{}, graphQLClock{})
	mux := http.NewServeMux()
	graphqldelivery.Register(mux, &graphqldelivery.Resolver{Users: svc}, func(next http.Handler) http.Handler { return auth.OptionalHTTP(manager, next) }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	id := uuid.NewString()
	response := postGraphQL(t, mux, "", `mutation($input:RegisterInput!){register(input:$input){token refreshToken refreshExpiresAt user{id}}}`, map[string]any{"input": map[string]any{"email": id + "@example.com", "username": "g" + id[:8], "password": "password123", "refreshSession": true}})
	if len(response.Errors) > 0 {
		t.Fatalf("register: %+v", response.Errors)
	}
	var first struct {
		Register struct{ Token, RefreshToken, RefreshExpiresAt string }
	}
	decodeData(t, response.Data, &first)
	if first.Register.RefreshToken == "" || first.Register.RefreshExpiresAt == "" {
		t.Fatal("missing refresh credential")
	}
	response = postGraphQL(t, mux, "", `mutation($token:String!){refreshSession(refreshToken:$token){token refreshToken user{id}}}`, map[string]any{"token": first.Register.RefreshToken})
	if len(response.Errors) > 0 {
		t.Fatalf("refresh: %+v", response.Errors)
	}
	var second struct {
		RefreshSession struct{ Token, RefreshToken string }
	}
	decodeData(t, response.Data, &second)
	if second.RefreshSession.RefreshToken == first.Register.RefreshToken {
		t.Fatal("credential unchanged")
	}
	response = postGraphQL(t, mux, "", `mutation($token:String!){revokeRefreshSession(refreshToken:$token)}`, map[string]any{"token": second.RefreshSession.RefreshToken})
	if len(response.Errors) > 0 {
		t.Fatalf("revoke: %+v", response.Errors)
	}
	if _, err := manager.Validate(context.Background(), second.RefreshSession.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("access survived logout: %v", err)
	}
	response = postGraphQL(t, mux, "", `mutation($input:LoginInput!){login(input:$input){refreshToken refreshExpiresAt}}`, map[string]any{"input": map[string]any{"email": id + "@example.com", "password": "password123"}})
	if len(response.Errors) > 0 {
		t.Fatalf("legacy login: %+v", response.Errors)
	}
	var legacy struct {
		Login struct{ RefreshToken, RefreshExpiresAt *string }
	}
	decodeData(t, response.Data, &legacy)
	if legacy.Login.RefreshToken != nil || legacy.Login.RefreshExpiresAt != nil {
		t.Fatal("legacy login opted into refresh")
	}
}

type refreshIDs struct{}

func (refreshIDs) New() string { return uuid.NewString() }

func TestRefreshCancellationAndLogoutRaces(t *testing.T) {
	for _, scenario := range []string{"cancel", "access_logout", "refresh_logout", "expired_access"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pool := throttlePool(t)
			repo := postgresrepo.NewUserRepository(pool)
			id := uuid.NewString()
			user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("c" + id[:8]), PasswordHash: "hash", Now: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if err = repo.Create(ctx, user); err != nil {
				t.Fatal(err)
			}
			ttl := time.Hour
			if scenario == "expired_access" {
				ttl = -time.Second
			}
			manager := auth.NewManager("test-secret", "users", ttl)
			manager.SetSessionStore(repo)
			manager.SetRefreshStore(repo)
			tokens, err := manager.IssueRefreshSession(ctx, user)
			if err != nil {
				t.Fatal(err)
			}
			if remaining := time.Until(tokens.RefreshExpiresAt); remaining < 30*24*time.Hour-time.Minute || remaining > 30*24*time.Hour {
				t.Fatalf("initial lifetime: %v", remaining)
			}
			hash := sha256.Sum256([]byte(tokens.RefreshToken))
			var persisted []byte
			if err = pool.QueryRow(ctx, `SELECT token_hash FROM user_refresh_tokens WHERE token_hash=$1`, hash[:]).Scan(&persisted); err != nil || len(persisted) != 32 || string(persisted) == tokens.RefreshToken {
				t.Fatalf("hash storage: %v", err)
			}
			if scenario == "expired_access" {
				if _, err = manager.Validate(ctx, tokens.AccessToken); !errors.Is(err, domain.ErrUnauthorized) {
					t.Fatal("access not expired")
				}
				if _, err = manager.RefreshSession(ctx, tokens.RefreshToken); err != nil {
					t.Fatalf("expired access blocked refresh: %v", err)
				}
				return
			}
			if scenario == "cancel" {
				tx, e := pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer func() { _ = tx.Rollback(ctx) }()
				if _, e = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id); e != nil {
					t.Fatal(e)
				}
				timeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
				if _, e = manager.RefreshSession(timeout, tokens.RefreshToken); !errors.Is(e, context.DeadlineExceeded) {
					t.Fatalf("cancelled wait: %v", e)
				}
				if e = tx.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				if _, e = manager.RefreshSession(ctx, tokens.RefreshToken); e != nil {
					t.Fatalf("cancel consumed original: %v", e)
				}
				return
			}
			start := make(chan struct{})
			rotation := make(chan error, 1)
			logout := make(chan error, 1)
			go func() { <-start; _, e := manager.RefreshSession(ctx, tokens.RefreshToken); rotation <- e }()
			go func() {
				<-start
				if scenario == "access_logout" {
					logout <- manager.Revoke(ctx, tokens.AccessToken)
				} else {
					logout <- manager.RevokeRefreshSession(ctx, tokens.RefreshToken)
				}
			}()
			close(start)
			if e := <-logout; e != nil {
				t.Fatalf("logout: %v", e)
			}
			if e := <-rotation; e != nil && !errors.Is(e, domain.ErrUnauthorized) {
				t.Fatalf("rotation: %v", e)
			}
			var active int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1 AND revoked_at IS NULL`, id).Scan(&active); err != nil || active != 0 {
				t.Fatalf("logout race survived: %d %v", active, err)
			}
		})
	}
}
