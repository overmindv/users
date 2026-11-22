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

func TestRecoveryPostgres(t *testing.T) {
	for _, scenario := range []string{"reset", "verify", "expired", "purpose", "version"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pool := throttlePool(t)
			repo := postgresrepo.NewUserRepository(pool)
			id := uuid.NewString()
			user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("r" + id[:8]), PasswordHash: "old-hash", Now: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if err = repo.Create(ctx, user); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(uuid.NewString()))
			purpose := domain.RecoveryReset
			if scenario == "verify" {
				purpose = domain.RecoveryVerify
			}
			if err = repo.QueueRecovery(ctx, user.Email().String(), purpose, hash[:], []byte("encrypted")); err != nil {
				t.Fatal(err)
			}
			session, family := uuid.NewString(), uuid.NewString()
			refresh := sha256.Sum256([]byte(uuid.NewString()))
			if _, err = repo.CreateRefreshSession(ctx, user, family, session, refresh[:], time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "expired":
				_, err = pool.Exec(ctx, "UPDATE user_recovery_tokens SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1", hash[:])
			case "version":
				user.SetPassword("different-hash", time.Now())
				err = repo.UpdatePassword(ctx, user)
			case "purpose":
				purpose = domain.RecoveryVerify
			}
			if err != nil {
				t.Fatal(err)
			}
			err = repo.ConsumeRecovery(ctx, hash[:], purpose, "new-hash")
			if scenario == "reset" || scenario == "verify" {
				if err != nil {
					t.Fatal(err)
				}
				if err = repo.ConsumeRecovery(ctx, hash[:], purpose, "new-hash"); !errors.Is(err, domain.ErrInvalidRecoveryToken) {
					t.Fatal("replay accepted", err)
				}
			} else if !errors.Is(err, domain.ErrInvalidRecoveryToken) {
				t.Fatal("invalid token accepted", err)
			}
			if scenario == "reset" {
				if _, err = repo.CheckSession(ctx, id, session, user.SecurityVersion()); !errors.Is(err, domain.ErrUnauthorized) {
					t.Fatal("access survived", err)
				}
				next := sha256.Sum256([]byte(uuid.NewString()))
				if _, err = repo.RotateRefreshSession(ctx, refresh[:], next[:], uuid.NewString(), time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrUnauthorized) {
					t.Fatal("refresh survived", err)
				}
			}
			if scenario == "verify" {
				var verified bool
				if err = pool.QueryRow(ctx, "SELECT email_verified FROM users WHERE id=$1", id).Scan(&verified); err != nil || !verified {
					t.Fatal(verified, err)
				}
			}
		})
	}
}

func TestRecoveryOutboxFencing(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	queue := postgresrepo.NewRecoveryOutbox(pool)
	id := uuid.NewString()
	user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("q" + id[:8]), PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(uuid.NewString()))
	if err = repo.QueueRecovery(ctx, user.Email().String(), domain.RecoveryReset, hash[:], []byte("encrypted")); err != nil {
		t.Fatal(err)
	}
	// Отдельная component БД содержит и предыдущие fixtures: изолируем окно claim
	// только сроками своих mail, не изменяя чужие rows.
	var mailID string
	if err = pool.QueryRow(ctx, "SELECT id FROM recovery_mail_outbox WHERE token_hash=$1", hash[:]).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE recovery_mail_outbox SET next_attempt_at='2000-01-01' WHERE id=$1", mailID); err != nil {
		t.Fatal(err)
	}
	first, found, err := queue.Claim(ctx)
	if err != nil || !found || first.ID != mailID {
		t.Fatal("claim", found, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE recovery_mail_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", mailID); err != nil {
		t.Fatal(err)
	}
	second, found, err := queue.Claim(ctx)
	if err != nil || !found || second.ID != mailID || first.ClaimToken == second.ClaimToken {
		t.Fatal("reclaim", found, err)
	}
	if err = queue.Complete(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err = queue.Retry(ctx, first); err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err = pool.QueryRow(ctx, "SELECT sent_at IS NULL AND claim_token=$2::uuid FROM recovery_mail_outbox WHERE id=$1", mailID, second.ClaimToken).Scan(&pending); err != nil || !pending {
		t.Fatal("stale worker changed claim", err)
	}
	if err = queue.Retry(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE recovery_mail_outbox SET next_attempt_at='2000-01-01',attempts=7 WHERE id=$1", mailID); err != nil {
		t.Fatal(err)
	}
	last, found, err := queue.Claim(ctx)
	if err != nil || !found || last.ID != mailID {
		t.Fatal("last claim", err)
	}
	if err = queue.Retry(ctx, last); err != nil {
		t.Fatal(err)
	}
	var exhausted bool
	if err = pool.QueryRow(ctx, "SELECT exhausted FROM recovery_mail_outbox WHERE id=$1", mailID).Scan(&exhausted); err != nil || !exhausted {
		t.Fatal("unbounded retry", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE user_recovery_tokens SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1", hash[:]); err != nil {
		t.Fatal(err)
	}
	if err = queue.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM recovery_mail_outbox WHERE id=$1", mailID).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired encrypted token retained", count, err)
	}
}
