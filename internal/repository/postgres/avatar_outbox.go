package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const AvatarMaxAttempts = 8

var ErrAvatarLeaseLost = errors.New("avatar lease lost")

type AvatarEvent struct {
	ID       string
	UserID   string
	FileID   *string
	Version  int64
	Attempts int
}

type AvatarOutbox struct{ pool *pgxpool.Pool }

func NewAvatarOutbox(pool *pgxpool.Pool) *AvatarOutbox { return &AvatarOutbox{pool: pool} }

// Claim ограничивает число попыток и всегда читает текущий снимок Users.
func (r *AvatarOutbox) Claim(ctx context.Context) (AvatarEvent, bool, error) {
	if _, err := r.pool.Exec(ctx, `WITH exhausted AS (
 SELECT id FROM user_outbox_events WHERE event_type='user.avatar_changed' AND attempts >= 8
 AND (status='pending' OR (status='running' AND locked_at<NOW()-INTERVAL '5 minutes'))
 ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 100)
 UPDATE user_outbox_events e SET status='failed',last_error='attempts_exhausted',updated_at=NOW()
 FROM exhausted x WHERE e.id=x.id`); err != nil {
		return AvatarEvent{}, false, err
	}
	var e AvatarEvent
	err := r.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT event.id FROM user_outbox_events event
 WHERE event.event_type='user.avatar_changed' AND event.attempts < 8
 AND ((event.status='pending' AND event.available_at<=NOW()) OR (event.status='running' AND event.locked_at<NOW()-INTERVAL '5 minutes'))
 AND EXISTS (SELECT 1 FROM users WHERE id=event.aggregate_id)
 ORDER BY event.available_at,event.created_at,event.id FOR UPDATE SKIP LOCKED LIMIT 1
 ), claimed AS (
 UPDATE user_outbox_events e SET status='running',locked_at=NOW(),attempts=attempts+1,updated_at=NOW()
 FROM candidate c WHERE e.id=c.id RETURNING e.id,e.aggregate_id,e.attempts)
 SELECT c.id,c.aggregate_id,c.attempts,u.avatar_version,CASE WHEN u.deleted_at IS NULL THEN u.avatar_file_id ELSE NULL END
 FROM claimed c JOIN users u ON u.id=c.aggregate_id`).Scan(&e.ID, &e.UserID, &e.Attempts, &e.Version, &e.FileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AvatarEvent{}, false, nil
	}
	if err != nil {
		return AvatarEvent{}, false, fmt.Errorf("claim avatar outbox: %w", err)
	}

	return e, true, nil
}

// Complete не принимает результат просроченной или перехваченной попытки.
func (r *AvatarOutbox) Complete(ctx context.Context, e AvatarEvent) error {
	result, err := r.pool.Exec(ctx, `UPDATE user_outbox_events SET status='published',updated_at=NOW(),locked_at=NULL
 WHERE id=$1 AND attempts=$2 AND status='running' AND locked_at>=NOW()-INTERVAL '5 minutes'`, e.ID, e.Attempts)
	if err != nil {
		return fmt.Errorf("complete avatar outbox: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrAvatarLeaseLost
	}

	return nil
}

// Retry не хранит ответ upstream: в нём могут находиться приватные данные.
func (r *AvatarOutbox) Retry(ctx context.Context, e AvatarEvent, _ error) error {
	delay := time.Duration(1<<min(e.Attempts, 8)) * time.Second
	result, err := r.pool.Exec(ctx, `UPDATE user_outbox_events SET
 status=CASE WHEN attempts>=8 THEN 'failed' ELSE 'pending' END,
 available_at=NOW()+$3::interval,last_error='media_delivery_failed',locked_at=NULL,updated_at=NOW()
 WHERE id=$1 AND attempts=$2 AND status='running' AND locked_at>=NOW()-INTERVAL '5 minutes'`, e.ID, e.Attempts, delay.String())
	if err != nil {
		return fmt.Errorf("retry avatar outbox: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrAvatarLeaseLost
	}

	return nil
}
