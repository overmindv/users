//go:build component

package integration

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

// TestRefreshRotationAndReuse проверяет транзакционную одноразовость и отзыв всей family.
func TestRefreshRotationAndReuse(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	id := uuid.NewString()
	user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("r" + id[:8]), PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	old := sha256.Sum256([]byte(uuid.NewString()))
	next := sha256.Sum256([]byte(uuid.NewString()))
	session := uuid.NewString()
	family := uuid.NewString()
	accessExpiry := time.Now().Add(time.Hour)
	refreshExpiry := time.Now().Add(24 * time.Hour)
	if _, err = repo.CreateRefreshSession(ctx, user, family, session, old[:], accessExpiry, refreshExpiry); err != nil {
		t.Fatal(err)
	}
	newSession := uuid.NewString()
	grant, err := repo.RotateRefreshSession(ctx, old[:], next[:], newSession, accessExpiry)
	if err != nil {
		t.Fatal(err)
	}
	if grant.UserID != id || grant.SecurityVersion != user.SecurityVersion() {
		t.Fatal("wrong identity")
	}
	if _, err = repo.CheckSession(ctx, id, newSession, user.SecurityVersion()); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RotateRefreshSession(ctx, old[:], next[:], uuid.NewString(), accessExpiry); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("reused refresh: %v", err)
	}
	for _, sid := range []string{session, newSession} {
		if _, err = repo.CheckSession(ctx, id, sid, user.SecurityVersion()); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("family session survived reuse: %v", err)
		}
	}
	if _, err = repo.RotateRefreshSession(ctx, next[:], old[:], uuid.NewString(), accessExpiry); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("revoked family refreshed: %v", err)
	}
}

// TestRefreshFamilyBoundaries покрывает отзыв, сроки и откат неудачной ротации.
func TestRefreshFamilyBoundaries(t *testing.T) {
	for _, scenario := range []string{"concurrent", "expired", "logout", "refresh_logout", "password", "roles", "deleted", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pool := throttlePool(t)
			repo := postgresrepo.NewUserRepository(pool)
			id := uuid.NewString()
			user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("r" + id[:8]), PasswordHash: "hash", Now: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if err = repo.Create(ctx, user); err != nil {
				t.Fatal(err)
			}
			old := sha256.Sum256([]byte(uuid.NewString()))
			next := sha256.Sum256([]byte(uuid.NewString()))
			other := sha256.Sum256([]byte(uuid.NewString()))
			sid, family := uuid.NewString(), uuid.NewString()
			expiry := time.Now().Add(time.Hour)
			if _, err = repo.CreateRefreshSession(ctx, user, family, sid, old[:], expiry, expiry); err != nil {
				t.Fatal(err)
			}
			otherSID := uuid.NewString()
			if _, err = repo.CreateRefreshSession(ctx, user, uuid.NewString(), otherSID, other[:], expiry, expiry); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "concurrent":
				start := make(chan struct{})
				results := make(chan error, 2)
				for i := 0; i < 2; i++ {
					go func() {
						<-start
						hash := sha256.Sum256([]byte(uuid.NewString()))
						_, e := repo.RotateRefreshSession(ctx, old[:], hash[:], uuid.NewString(), expiry)
						results <- e
					}()
				}
				close(start)
				a, b := <-results, <-results
				if !((a == nil && errors.Is(b, domain.ErrUnauthorized)) || (b == nil && errors.Is(a, domain.ErrUnauthorized))) {
					t.Fatalf("rotation race: %v / %v", a, b)
				}
				var active int
				if err = pool.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE refresh_family_id=$1 AND revoked_at IS NULL`, family).Scan(&active); err != nil || active != 0 {
					t.Fatalf("winner session survived: %d %v", active, err)
				}
			case "expired":
				_, err = pool.Exec(ctx, `UPDATE user_refresh_families SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, family)
			case "logout":
				err = repo.RevokeSession(ctx, id, sid)
			case "refresh_logout":
				err = repo.RevokeRefreshSession(ctx, old[:])
				if err == nil {
					err = repo.RevokeRefreshSession(ctx, old[:])
				}
			case "password":
				_, err = pool.Exec(ctx, `UPDATE users SET password_hash='changed' WHERE id=$1`, id)
			case "roles":
				_, err = pool.Exec(ctx, `UPDATE users SET roles=ARRAY['user','admin'] WHERE id=$1`, id)
			case "deleted":
				_, err = pool.Exec(ctx, `UPDATE users SET deleted_at=clock_timestamp() WHERE id=$1`, id)
			case "rollback":
				if _, err = repo.RotateRefreshSession(ctx, old[:], next[:], sid, expiry); err == nil {
					t.Fatal("duplicate session accepted")
				}
				grant, e := repo.RotateRefreshSession(ctx, old[:], next[:], uuid.NewString(), expiry.Add(time.Hour))
				if e != nil {
					t.Fatalf("old token consumed on rollback: %v", e)
				}
				if grant.AccessExpiresAt.After(expiry) || grant.RefreshExpiresAt.After(expiry) {
					t.Fatal("absolute lifetime extended")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.RotateRefreshSession(ctx, old[:], next[:], uuid.NewString(), expiry); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("invalid family refreshed: %v", err)
			}
			if scenario != "password" && scenario != "roles" && scenario != "deleted" {
				if _, err = repo.CheckSession(ctx, id, otherSID, user.SecurityVersion()); err != nil {
					t.Fatalf("other device affected: %v", err)
				}
			}
		})
	}
}
