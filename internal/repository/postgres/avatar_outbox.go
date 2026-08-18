package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AvatarEvent struct {
	ID       string
	UserID   string
	FileID   *string
	Attempts int
}

type AvatarOutbox struct {
	pool *pgxpool.Pool
}

// NewAvatarOutbox создаёт repository transactional outbox аватаров.
func NewAvatarOutbox(pool *pgxpool.Pool) *AvatarOutbox { return &AvatarOutbox{pool: pool} }

// Claim захватывает следующее доступное событие через SKIP LOCKED.
func (r *AvatarOutbox) Claim(ctx context.Context) (AvatarEvent, bool, error) {
	var event AvatarEvent
	var payload []byte
	err := r.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT event.id FROM user_outbox_events event
			 WHERE ((event.status='pending' AND event.available_at<=NOW())
			        OR (event.status='running' AND event.locked_at<NOW()-INTERVAL '5 minutes'))
			   AND NOT EXISTS (
			       SELECT 1 FROM user_outbox_events previous
			        WHERE previous.aggregate_id=event.aggregate_id
			          AND previous.status IN ('pending','running')
			          AND (previous.created_at<event.created_at OR (previous.created_at=event.created_at AND previous.id<event.id))
			   )
			 ORDER BY event.available_at,event.created_at FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE user_outbox_events e SET status='running',locked_at=NOW(),attempts=attempts+1,updated_at=NOW()
		FROM candidate c WHERE e.id=c.id
		RETURNING e.id,e.aggregate_id,e.payload,e.attempts`).Scan(&event.ID, &event.UserID, &payload, &event.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return AvatarEvent{}, false, nil
	}
	if err != nil {
		return AvatarEvent{}, false, fmt.Errorf("claim avatar outbox: %w", err)
	}
	var decoded struct {
		FileID *string `json:"file_id"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return AvatarEvent{}, false, fmt.Errorf("decode avatar outbox: %w", err)
	}
	event.FileID = decoded.FileID

	return event, true, nil
}

// Complete помечает успешно доставленное событие опубликованным.
func (r *AvatarOutbox) Complete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_outbox_events SET status='published',updated_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("complete avatar outbox: %w", err)
	}

	return nil
}

// Retry возвращает событие в очередь с ограниченным exponential backoff.
func (r *AvatarOutbox) Retry(ctx context.Context, event AvatarEvent, cause error) error {
	delay := time.Duration(1<<min(event.Attempts, 8)) * time.Second
	_, err := r.pool.Exec(ctx, `
		UPDATE user_outbox_events
		   SET status='pending',available_at=NOW()+$2::interval,last_error=$3,updated_at=NOW()
		 WHERE id=$1`, event.ID, delay.String(), truncate(cause.Error(), 1000))
	if err != nil {
		return fmt.Errorf("retry avatar outbox: %w", err)
	}

	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit]
}
