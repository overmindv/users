//go:build component

package integration

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

func TestAvatarDeliveryFencing(t *testing.T) {
	ctx := t.Context()
	dsn, err := url.Parse(os.Getenv("COMPONENT_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := parker.OpenPool(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "avatar_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	params := dsn.Query()
	// Собственная история миграций исключает fallback на public.goose_db_version.
	if _, err = admin.Exec(ctx, "CREATE TABLE "+schema+".goose_db_version (id bigserial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp NOT NULL DEFAULT now()); INSERT INTO "+schema+".goose_db_version(version_id,is_applied) VALUES (0,true)"); err != nil {
		t.Fatal(err)
	}
	params.Set("search_path", schema+",public")
	dsn.RawQuery = params.Encode()
	if err = parker.RunMigrate("../../migrations", dsn.String(), "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := parker.OpenPool(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := postgresrepo.NewUserRepository(pool)
	id := uuid.NewString()
	suffix := id[:8]
	user, err := domain.NewUser(domain.NewUserParams{ID: id, Email: domain.Email(suffix + "@example.org"), Username: domain.Username("av_" + suffix), PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	outbox := postgresrepo.NewAvatarOutbox(pool)
	a, b, c := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`UPDATE users SET avatar_file_id=$2 WHERE id=$1`, id, a)
	first, found, err := outbox.Claim(ctx)
	if err != nil || !found {
		t.Fatalf("claim %v %v", found, err)
	}
	exec(`UPDATE user_outbox_events SET locked_at=NOW()-INTERVAL '6 minutes' WHERE id=$1`, first.ID)
	if err = outbox.Complete(ctx, first); err == nil {
		t.Fatal("expired completion accepted before reclaim")
	}
	second, found, err := outbox.Claim(ctx)
	if err != nil || !found || second.Attempts != first.Attempts+1 {
		t.Fatalf("reclaim %+v %v", second, err)
	}
	if err = outbox.Complete(ctx, first); err == nil {
		t.Fatal("stale completion accepted")
	}
	if err = outbox.Retry(ctx, first, errors.New("secret upstream response")); err == nil {
		t.Fatal("stale retry accepted")
	}
	exec(`UPDATE users SET avatar_file_id=$2 WHERE id=$1`, id, b)
	exec(`UPDATE users SET avatar_file_id=$2 WHERE id=$1`, id, c)
	if err = outbox.Complete(ctx, second); err != nil {
		t.Fatal(err)
	}
	current, found, err := outbox.Claim(ctx)
	if err != nil || !found || current.FileID == nil || *current.FileID != c {
		t.Fatalf("current snapshot %+v %v", current, err)
	}
	exec(`UPDATE user_outbox_events SET attempts=8 WHERE id=$1`, current.ID)
	current.Attempts = 8
	if err = outbox.Retry(ctx, current, errors.New("secret upstream response")); err != nil {
		t.Fatal(err)
	}
	var status, lastError string
	if err = pool.QueryRow(ctx, `SELECT status,last_error FROM user_outbox_events WHERE id=$1`, current.ID).Scan(&status, &lastError); err != nil || status != "failed" || lastError != "media_delivery_failed" {
		t.Fatalf("retry %s %q %v", status, lastError, err)
	}
	// Потеря всех событий текущего пользователя: bounded reconciliation восстанавливает снимок.
	exec(`DELETE FROM user_outbox_events WHERE aggregate_id=$1`, id)
	if _, err = outbox.Reconcile(ctx, ""); err != nil {
		t.Fatal(err)
	}
	restored, found, err := outbox.Claim(ctx)
	if err != nil || !found || restored.FileID == nil || *restored.FileID != c {
		t.Fatalf("reconcile %+v %v", restored, err)
	}
	if err = outbox.Complete(ctx, restored); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`, id)
	exec(`SELECT users_replay_avatar($1)`, id)
	removed, found, err := outbox.Claim(ctx)
	if err != nil || !found || removed.FileID != nil || removed.Version <= restored.Version {
		t.Fatalf("delete replay %+v %v", removed, err)
	}
	for attempt := 1; attempt < postgresrepo.AvatarMaxAttempts; attempt++ {
		if removed.Attempts != attempt {
			t.Fatalf("attempt %d want %d", removed.Attempts, attempt)
		}
		if err = outbox.Retry(ctx, removed, errors.New("outage")); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE user_outbox_events SET available_at=NOW() WHERE id=$1`, removed.ID)
		removed, found, err = outbox.Claim(ctx)
		if err != nil || !found {
			t.Fatalf("retry claim %v %v", found, err)
		}
	}
	// Сбой процесса на последней попытке также исчерпывает бюджет.
	exec(`UPDATE user_outbox_events SET locked_at=NOW()-INTERVAL '6 minutes' WHERE id=$1`, removed.ID)
	if _, found, err = outbox.Claim(ctx); err != nil || found {
		t.Fatalf("ninth attempt %v %v", found, err)
	}
	if _, err = outbox.Reconcile(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, found, err = outbox.Claim(ctx); err != nil || found {
		t.Fatalf("automatic failed replay %v %v", found, err)
	}
	exec(`SELECT users_replay_avatar($1)`, id)
	replayed, found, err := outbox.Claim(ctx)
	if err != nil || !found || replayed.FileID != nil || replayed.Version != removed.Version || replayed.Attempts != 1 {
		t.Fatalf("safe replay %+v %v", replayed, err)
	}
	if err = outbox.Complete(ctx, replayed); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO user_outbox_events(id,aggregate_id,event_type,payload) VALUES(gen_random_uuid(),$1,'user.deleted.v1','{}')`, id)
	if _, found, err = outbox.Claim(ctx); err != nil || found {
		t.Fatalf("claimed foreign event %v %v", found, err)
	}
}
