//go:build component

package integration

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"testing"
	"time"
)

func TestPostgresSessions(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	for _, mutation := range []string{"roles=ARRAY['user','admin']::text[]", "password_hash='newhash'", "deleted_at=clock_timestamp()"} {
		t.Run(mutation, func(t *testing.T) {
			id := uuid.NewString()
			user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("u" + id[:8]), PasswordHash: "hash", Now: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if err = repo.Create(ctx, user); err != nil {
				t.Fatal(err)
			}
			m := auth.NewManager("secret", "users", time.Hour)
			m.SetSessionStore(repo)
			a, _, err := m.IssueUser(ctx, user)
			if err != nil {
				t.Fatal(err)
			}
			b, _, err := m.IssueUser(ctx, user)
			if err != nil {
				t.Fatal(err)
			}
			if err = m.Revoke(ctx, a); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Validate(ctx, a); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("revoked: %v", err)
			}
			if _, err = m.Validate(ctx, b); err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "UPDATE users SET "+mutation+" WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Validate(ctx, b); err != nil {
				t.Fatalf("rollback revoked: %v", err)
			}
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "UPDATE users SET "+mutation+" WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, _, err := m.IssueUser(ctx, user); done <- err }()
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("stale login: %v", err)
			}
			if _, err = m.Validate(ctx, b); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("security change accepted: %v", err)
			}
		})
	}
}

func TestPostgresPasswordUpgradeRejectsStaleHash(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	id := uuid.NewString()
	user, _ := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("u" + id[:8]), PasswordHash: "old", Now: time.Now()})
	if err := repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	user.SetPassword("new", time.Now())
	if err := repo.UpdatePassword(ctx, user); err != nil {
		t.Fatal(err)
	}
	user.SetPassword("rehash-old", time.Now())
	if err := repo.UpgradePassword(ctx, user, "old"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("stale rehash accepted: %v", err)
	}
	got, err := repo.GetByID(ctx, id)
	if err != nil || got.PasswordHash() != "new" {
		t.Fatalf("password overwritten: %v", err)
	}
}

func TestPostgresSessionRejectsRestoredRoles(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	backup := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,username,roles,created_at,updated_at) VALUES($1,$2,'$argon2id$test',$3,ARRAY['admin'],now(),now())`, backup, backup+"@example.com", "b"+backup[:8]); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	u, _ := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("u" + id[:8]), PasswordHash: "hash", Now: time.Now()})
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	for _, roles := range [][]string{{"admin"}, u.Roles()} {
		if _, err := pool.Exec(ctx, `UPDATE users SET roles=$2 WHERE id=$1`, id, roles); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.CreateSession(ctx, u, uuid.NewString(), time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("ABA accepted: %v", err)
	}
}
