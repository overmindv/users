//go:build component

package integration

import (
	"context"
	"crypto/sha256"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

func TestPrivacyDeletionAtomic(t *testing.T) {
	ctx := t.Context()
	pool := privacyPool(t)
	repo := postgresrepo.NewUserRepository(pool)
	id := uuid.NewString()
	user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.org"), Username: domain.Username("p_" + id[:8]), PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	session := uuid.NewString()
	version, err := repo.CreateSession(ctx, user, session, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	proof := sha256.Sum256([]byte("fixture-receipt"))
	export, err := repo.PreparePrivacy(ctx, domain.PrivacyPreparation{ID: uuid.NewString(), UserID: id, SessionID: session, Kind: "export", PasswordHash: "hash", SecurityVersion: version, ReceiptHash: proof[:]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ConfirmPrivacy(ctx, export.ID, proof[:]); err != nil {
		t.Fatal(err)
	}
	exportJob, found, err := repo.ClaimPrivacyExport(ctx)
	if err != nil || !found {
		t.Fatal("export preparation", err)
	}
	prepared, err := repo.PreparePrivacy(ctx, domain.PrivacyPreparation{ID: uuid.NewString(), UserID: id, SessionID: session, Kind: "delete", PasswordHash: "hash", SecurityVersion: version, ReceiptHash: proof[:]})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		state, err := repo.ConfirmPrivacy(ctx, prepared.ID, proof[:])
		if err != nil || state.State != "processing" {
			t.Fatalf("confirm %+v %v", state, err)
		}
	}
	if _, err = repo.CheckSession(ctx, id, session, version); err == nil {
		t.Fatal("session survived")
	}
	if err = repo.SavePrivacyExport(ctx, exportJob, domain.ExportPage{UserID: id, Watermark: "old", Records: []domain.ExportRecord{}}); err == nil {
		t.Fatal("export resurrected after deletion")
	}
	var events, ledger int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM user_outbox_events WHERE event_type='user.deleted.v1' AND aggregate_id=$1`, id).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d %v", events, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM privacy_deletion_ledger WHERE user_id=$1`, id).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("ledger %d %v", ledger, err)
	}
	if _, err = repo.PrivacyStatus(ctx, prepared.ID, make([]byte, 32)); err == nil {
		t.Fatal("wrong receipt accepted")
	}
	if err = repo.AcknowledgePrivacy(ctx, prepared.ID, "tasks", "completed", ""); err != nil {
		t.Fatal(err)
	}
	if err = repo.AcknowledgePrivacy(ctx, prepared.ID, "tasks", "failed", "processing_failed"); err != nil {
		t.Fatal(err)
	}
	state, err := repo.PrivacyStatus(ctx, prepared.ID, proof[:])
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "processing" {
		t.Fatal("partial deletion declared complete")
	}
	for _, owner := range state.Owners {
		if owner.Owner == "tasks" && owner.State != "completed" {
			t.Fatal("ack regressed")
		}
	}
	page, err := repo.DeletionLedger(ctx, 0, 0, 1)
	if err != nil || len(page.Events) != 1 || !strings.Contains(string(page.Events[0]), id) {
		t.Fatal("deletion replay", err)
	}
	job, found, err := repo.ClaimDeletion(ctx)
	if err != nil || !found || job.ID != prepared.ID {
		t.Fatalf("deletion outbox %+v %v", job, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE user_outbox_events SET locked_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	newJob, found, err := repo.ClaimDeletion(ctx)
	if err != nil || !found || string(newJob.Payload) != string(job.Payload) {
		t.Fatal("unstable event replay", err)
	}
	if err = repo.FinishDeletion(ctx, job, true); err == nil {
		t.Fatal("stale publisher acknowledged")
	}
	if err = repo.FinishDeletion(ctx, newJob, true); err != nil {
		t.Fatal(err)
	}
	for _, owner := range domain.PrivacyOwners {
		if err = repo.AcknowledgePrivacy(ctx, prepared.ID, owner, "completed", ""); err != nil {
			t.Fatal(err)
		}
	}
	state, err = repo.PrivacyStatus(ctx, prepared.ID, proof[:])
	if err != nil || state.State != "completed" || state.Publication != "published" {
		t.Fatalf("completed %+v %v", state, err)
	}
}

func privacyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()
	dsn, err := url.Parse(os.Getenv("COMPONENT_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := parker.OpenPool(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "privacy_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if _, err = admin.Exec(ctx, "CREATE TABLE "+schema+".goose_db_version(id bigserial PRIMARY KEY,version_id bigint NOT NULL,is_applied boolean NOT NULL,tstamp timestamp NOT NULL DEFAULT now()); INSERT INTO "+schema+".goose_db_version(version_id,is_applied) VALUES(0,true)"); err != nil {
		t.Fatal(err)
	}
	params := dsn.Query()
	params.Set("search_path", schema+",public")
	dsn.RawQuery = params.Encode()
	if err = parker.RunMigrate("../../migrations", dsn.String(), "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := parker.OpenPool(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPrivacyExportRepeatAndExpiry(t *testing.T) {
	pool := privacyPool(t)
	ctx := t.Context()
	repo := postgresrepo.NewUserRepository(pool)
	id := uuid.NewString()
	user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(id + "@example.org"), Username: domain.Username("exp_" + id[:8]), PasswordHash: "do-not-export", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	session := uuid.NewString()
	version, err := repo.CreateSession(ctx, user, session, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	proof := sha256.Sum256([]byte("export-proof"))
	for range 2 {
		request, err := repo.PreparePrivacy(ctx, domain.PrivacyPreparation{ID: uuid.NewString(), UserID: id, SessionID: session, Kind: "export", PasswordHash: user.PasswordHash(), SecurityVersion: version, ReceiptHash: proof[:]})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = repo.ConfirmPrivacy(ctx, request.ID, proof[:]); err != nil {
			t.Fatal(err)
		}
		for range len(domain.PrivacyOwners) {
			job, found, err := repo.ClaimPrivacyExport(ctx)
			if err != nil || !found {
				t.Fatalf("claim %v %v", found, err)
			}
			stale := job
			stale.ClaimToken = uuid.NewString()
			page := domain.ExportPage{UserID: id, Watermark: "snapshot", Records: []domain.ExportRecord{}}
			if err = repo.SavePrivacyExport(ctx, stale, page); err == nil {
				t.Fatal("stale claim accepted")
			}
			if err = repo.SavePrivacyExport(ctx, job, page); err != nil {
				t.Fatal(err)
			}
		}
		result, err := repo.DownloadPrivacyExport(ctx, id, request.ID)
		if err != nil || len(result.Pages) != len(domain.PrivacyOwners) || strings.Contains(string(result.Profile), "do-not-export") {
			t.Fatal("export incomplete or secret", err)
		}
		if _, err = repo.DownloadPrivacyExport(ctx, uuid.NewString(), request.ID); err == nil {
			t.Fatal("foreign export downloaded")
		}
		if _, err = pool.Exec(ctx, `UPDATE privacy_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, request.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.DownloadPrivacyExport(ctx, id, request.ID); err == nil {
			t.Fatal("expired export downloaded")
		}
		if err = repo.ExpirePrivacyExports(ctx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM privacy_export_pages WHERE request_id=$1`, request.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("expiry %d %v", count, err)
		}
	}
	prepare := func() domain.PrivacyRequest {
		t.Helper()
		request, err := repo.PreparePrivacy(ctx, domain.PrivacyPreparation{ID: uuid.NewString(), UserID: id, SessionID: session, Kind: "delete", PasswordHash: user.PasswordHash(), SecurityVersion: version, ReceiptHash: proof[:]})
		if err != nil {
			t.Fatal(err)
		}
		return request
	}
	expired := prepare()
	if _, err = pool.Exec(ctx, `UPDATE privacy_requests SET confirm_until=clock_timestamp()-interval '1 second' WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ConfirmPrivacy(ctx, expired.ID, proof[:]); err == nil {
		t.Fatal("expired confirmation accepted")
	}
	stale := prepare()
	if _, err = pool.Exec(ctx, `UPDATE users SET password_hash='changed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ConfirmPrivacy(ctx, stale.ID, proof[:]); err == nil {
		t.Fatal("stale security version accepted")
	}
}
