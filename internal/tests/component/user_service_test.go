//go:build component

package component

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/arcee/internal/auth"
	"github.com/overmindv/arcee/internal/config"
	"github.com/overmindv/arcee/internal/domain"
	postgresrepo "github.com/overmindv/arcee/internal/repository/postgres"
	"github.com/overmindv/arcee/internal/security"
	"github.com/overmindv/arcee/internal/usecase"
)

type ids struct{}

func (ids) New() string { return uuid.NewString() }

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

func TestRegistrationLoginProfileUpdateAndSoftDelete(t *testing.T) {
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}

	ctx := context.Background()
	pool, err := postgresrepo.Open(ctx, config.Database{DSN: dsn, MaxConnections: 5, MinConnections: 1, MaxConnLifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, "TRUNCATE users"); err != nil {
		t.Fatal(err)
	}

	repository := postgresrepo.NewUserRepository(pool)
	service := usecase.NewUserService(repository, security.PlainTextHasher{}, auth.NewManager("component-secret", "arcee", 24*time.Hour), ids{}, clock{})
	registered, err := service.Register(ctx, usecase.RegisterInput{Email: "component@example.com", Password: "password", Username: "component_user", FirstName: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, usecase.LoginInput{Email: "component@example.com", Password: "password"}); err != nil {
		t.Fatal(err)
	}

	name, username := "New", "component_updated"
	updated, err := service.Update(ctx, usecase.UpdateUserInput{ID: registered.User.ID(), FirstName: &name, Username: &username})
	if err != nil {
		t.Fatal(err)
	}
	if updated.FirstName() != name || updated.Username().String() != username {
		t.Fatalf("profile not updated: %q %q", updated.FirstName(), updated.Username())
	}

	if err := service.Delete(ctx, registered.User.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, registered.User.ID()); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected soft-deleted user to be hidden, got %v", err)
	}

	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT deleted_at FROM users WHERE id=$1", registered.User.ID()).Scan(&deletedAt); err != nil {
		t.Fatal(err)
	}
	if deletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
}
