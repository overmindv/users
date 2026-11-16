//go:build component

package integration

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

func TestSecurityAuditRollbackAndLastAdmin(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	if _, err := pool.Exec(ctx, `TRUNCATE user_sessions,user_outbox_events,users`); err != nil {
		t.Fatal(err)
	}
	ids := []string{uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,username,roles,created_at,updated_at) VALUES($1,$2,'$argon2id$test',$3,ARRAY['admin'],now(),now())`, id, id+"@example.com", []string{"admin_one", "admin_two"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET roles=ARRAY[]::text[] WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM user_security_audit WHERE subject_id=$1 AND action='security_changed'`, ids[0]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback audit %d %v", count, err)
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, err := pool.Exec(ctx, `UPDATE users SET roles=ARRAY[]::text[] WHERE id=$1`, id); err == nil {
				success.Add(1)
			}
		}(id)
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("demoted %d", success.Load())
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE 'admin'=ANY(roles) AND deleted_at IS NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("remaining %d %v", count, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM user_security_audit`); err == nil {
		t.Fatal("audit deletion allowed")
	}
	if _, err = pool.Exec(ctx, `UPDATE user_security_audit SET action='tampered'`); err == nil {
		t.Fatal("audit update allowed")
	}
	if _, err = pool.Exec(ctx, `TRUNCATE user_security_audit`); err == nil {
		t.Fatal("audit truncate allowed")
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM user_security_audit WHERE details::text LIKE '%secret-hash%' OR details::text LIKE '%example.com%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("audit secret %d %v", count, err)
	}
}

func TestSecurityAuditActorAndPassword(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	id := uuid.NewString()
	actor := uuid.NewString()
	u, _ := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("u" + id[:8]), PasswordHash: "old", Now: time.Now()})
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	u.SetPassword("new-secret", time.Now())
	if err := repo.UpdatePassword(domain.WithAuditActor(ctx, actor), u); err != nil {
		t.Fatal(err)
	}
	var got, details string
	if err := pool.QueryRow(ctx, `SELECT actor_id::text,details::text FROM user_security_audit WHERE subject_id=$1 AND action='security_changed'`, id).Scan(&got, &details); err != nil || got != actor || strings.Contains(details, "new-secret") {
		t.Fatalf("audit actor/details: %s %v", got, err)
	}
}

func TestLastRealAdminIgnoresDemo(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	if _, err := pool.Exec(ctx, `TRUNCATE user_sessions,user_outbox_events,users`); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"00000000-0000-0000-0000-000000000009", uuid.NewString()} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,username,roles,created_at,updated_at) VALUES($1,$2,'$argon2id$test',$3,ARRAY['admin'],now(),now())`, id, id+"@example.com", []string{"seed_admin", "real_admin"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET roles=ARRAY[]::text[] WHERE username='real_admin'`); err == nil {
		t.Fatal("last usable administrator removed")
	}
}

func TestNonLocalRejectsExistingDemoSession(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	id := "00000000-0000-0000-0000-000000000010"
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,username,created_at,updated_at) VALUES($1,'session-demo@example.com','$argon2id$test','session_demo',now(),now()) ON CONFLICT(id) DO UPDATE SET password_hash=excluded.password_hash,deleted_at=NULL`, id); err != nil {
		t.Fatal(err)
	}
	u, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	m := auth.NewManager("secret", "users", time.Hour)
	m.SetSessionStore(repo)
	token, _, err := m.IssueUser(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	repo.SetLocalCredentials(false)
	if _, err = m.Validate(ctx, token); err == nil {
		t.Fatal("existing demo session accepted outside local")
	}
}
