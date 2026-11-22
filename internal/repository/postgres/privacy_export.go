package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/privacyowner"
)

type ExportJob struct {
	RequestID, UserID, Owner, Cursor, Watermark, ClaimToken string
	Attempts                                                int
}

func (r *UserRepository) ClaimPrivacyExport(ctx context.Context) (ExportJob, bool, error) {
	var job ExportJob
	// Падение последней попытки не должно создавать девятую.
	_, err := r.pool.Exec(ctx, `WITH exhausted AS (SELECT s.request_id,s.owner FROM privacy_owner_steps s JOIN privacy_requests r ON r.id=s.request_id
 WHERE r.kind='export' AND r.state='processing' AND s.state='running' AND s.lease_until<clock_timestamp() AND s.attempts>=8
 ORDER BY s.request_id,s.owner FOR UPDATE OF s SKIP LOCKED LIMIT 100)
 UPDATE privacy_owner_steps s SET state='failed',error_code='attempts_exhausted' FROM exhausted e WHERE s.request_id=e.request_id AND s.owner=e.owner`)
	if err != nil {
		return job, false, err
	}
	_, err = r.pool.Exec(ctx, `UPDATE privacy_requests r SET state='failed' WHERE kind='export' AND state='processing' AND EXISTS(SELECT 1 FROM privacy_owner_steps s WHERE s.request_id=r.id AND s.state='failed')`)
	if err != nil {
		return job, false, err
	}
	token := uuid.NewString()
	err = r.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT s.request_id,s.owner FROM privacy_owner_steps s JOIN privacy_requests r ON r.id=s.request_id JOIN users u ON u.id=r.user_id
 WHERE r.kind='export' AND r.state='processing' AND r.expires_at>clock_timestamp() AND u.deleted_at IS NULL AND s.attempts<8
 AND ((s.state='pending' AND s.available_at<=clock_timestamp()) OR (s.state='running' AND s.lease_until<clock_timestamp()))
 ORDER BY s.available_at,s.request_id,s.owner FOR UPDATE OF s SKIP LOCKED LIMIT 1)
 UPDATE privacy_owner_steps s SET state='running',attempts=attempts+1,claim_token=$1,lease_until=clock_timestamp()+interval '1 minute'
 FROM candidate c,privacy_requests r WHERE s.request_id=c.request_id AND s.owner=c.owner AND r.id=s.request_id
 RETURNING s.request_id,r.user_id,s.owner,s.cursor,s.watermark,s.attempts,s.claim_token`, token).Scan(&job.RequestID, &job.UserID, &job.Owner, &job.Cursor, &job.Watermark, &job.Attempts, &job.ClaimToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, false, nil
	}

	return job, err == nil, err
}

func (r *UserRepository) SavePrivacyExport(ctx context.Context, job ExportJob, page domain.ExportPage) error {
	if err := privacyowner.ValidatePage(job.Owner, job.UserID, job.Cursor, job.Watermark, page); err != nil {
		return err
	}
	payload, err := json.Marshal(page)
	if err != nil {
		return err
	}
	if len(payload) > privacyowner.MaxPageBytes {
		return domain.ErrPrivacyRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var active bool
	if err = tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM users WHERE id=$1 FOR UPDATE`, job.UserID).Scan(&active); err != nil || !active {
		return domain.ErrPrivacyRequest
	}
	var state string
	if err = tx.QueryRow(ctx, `SELECT state FROM privacy_requests WHERE id=$1 AND user_id=$2 AND kind='export' AND expires_at>clock_timestamp() FOR UPDATE`, job.RequestID, job.UserID).Scan(&state); err != nil || state != "processing" {
		return domain.ErrPrivacyRequest
	}
	var pages int
	var cursor string
	err = tx.QueryRow(ctx, `SELECT pages,cursor FROM privacy_owner_steps WHERE request_id=$1 AND owner=$2 AND state='running' AND claim_token=$3 AND lease_until>clock_timestamp() FOR UPDATE`, job.RequestID, job.Owner, job.ClaimToken).Scan(&pages, &cursor)
	if err != nil || cursor != job.Cursor {
		return domain.ErrPrivacyRequest
	}
	var bytes int64
	var reused bool
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(bytes),0) FROM privacy_owner_steps WHERE request_id=$1`, job.RequestID).Scan(&bytes); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM privacy_export_pages WHERE request_id=$1 AND owner=$2 AND payload->>'next_cursor'=$3)`, job.RequestID, job.Owner, page.NextCursor).Scan(&reused); err != nil {
		return err
	}
	if pages >= 1000 || bytes+int64(len(payload)) > 10<<20 || (page.NextCursor != "" && reused) {
		return domain.ErrPrivacyRequest
	}
	if _, err = tx.Exec(ctx, `INSERT INTO privacy_export_pages(request_id,owner,page,payload) VALUES($1,$2,$3,$4)`, job.RequestID, job.Owner, pages, payload); err != nil {
		return err
	}
	nextState := privacyPending
	if page.NextCursor == "" {
		nextState = privacyCompleted
	}
	_, err = tx.Exec(ctx, `UPDATE privacy_owner_steps SET state=$3,cursor=$4,watermark=$5,pages=pages+1,bytes=bytes+$6,
 attempts=0,error_code='',claim_token=NULL,lease_until=NULL,available_at=clock_timestamp() WHERE request_id=$1 AND owner=$2`, job.RequestID, job.Owner, nextState, page.NextCursor, page.Watermark, len(payload))
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE privacy_requests SET state='completed' WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM privacy_owner_steps WHERE request_id=$1 AND state<>'completed')`, job.RequestID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *UserRepository) RetryPrivacyExport(ctx context.Context, job ExportJob) error {
	delay := time.Duration(1<<min(job.Attempts, 8)) * time.Second
	_, err := r.pool.Exec(ctx, `UPDATE privacy_owner_steps SET state=CASE WHEN attempts>=8 THEN 'failed' ELSE 'pending' END,
 error_code='owner_export_failed',available_at=clock_timestamp()+$4::interval,claim_token=NULL,lease_until=NULL
 WHERE request_id=$1 AND owner=$2 AND claim_token=$3 AND state='running' AND lease_until>clock_timestamp()`, job.RequestID, job.Owner, job.ClaimToken, delay.String())

	return err
}

type PrivacyExport struct {
	Profile json.RawMessage                `json:"profile"`
	Pages   map[string][]domain.ExportPage `json:"services"`
}

func (r *UserRepository) DownloadPrivacyExport(ctx context.Context, userID, id string) (PrivacyExport, error) {
	result := PrivacyExport{Pages: map[string][]domain.ExportPage{}}
	// Один snapshot не допускает выдачу половины артефакта при concurrent cleanup.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `SELECT r.profile FROM privacy_requests r JOIN users u ON u.id=r.user_id WHERE r.id=$1 AND r.user_id=$2 AND r.kind='export' AND r.state='completed' AND r.expires_at>clock_timestamp() AND u.deleted_at IS NULL`, id, userID).Scan(&result.Profile)
	if err != nil {
		return result, domain.ErrPrivacyRequest
	}
	rows, err := tx.Query(ctx, `SELECT owner,payload FROM privacy_export_pages WHERE request_id=$1 ORDER BY owner,page`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var owner string
		var payload []byte
		if err = rows.Scan(&owner, &payload); err != nil {
			rows.Close()

			return result, err
		}
		var page domain.ExportPage
		if err = json.Unmarshal(payload, &page); err != nil {
			rows.Close()

			return result, err
		}
		result.Pages[owner] = append(result.Pages[owner], page)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}

	return result, tx.Commit(ctx)
}

func (r *UserRepository) ExpirePrivacyExports(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT id FROM privacy_requests WHERE kind='export' AND expires_at<=clock_timestamp() AND (profile IS NOT NULL OR state<>'expired') ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT 100`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()

			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM privacy_export_pages WHERE request_id=ANY($1::uuid[])`, ids); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE privacy_requests SET state='expired',profile=NULL WHERE id=ANY($1::uuid[])`, ids); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
