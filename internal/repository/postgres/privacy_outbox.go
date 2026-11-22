package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type DeletionJob struct {
	ID, UserID, ClaimToken string
	Payload                []byte
	Attempts               int
}

var ErrPrivacyLeaseLost = errors.New("privacy lease lost")

func (r *UserRepository) ClaimDeletion(ctx context.Context) (DeletionJob, bool, error) {
	var job DeletionJob
	_, err := r.pool.Exec(ctx, `WITH exhausted AS(SELECT id FROM user_outbox_events WHERE event_type='user.deleted.v1' AND status='running' AND attempts>=8 AND locked_at<clock_timestamp()-interval '1 minute' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 100)
 UPDATE user_outbox_events e SET status='failed',last_error='attempts_exhausted' FROM exhausted x WHERE e.id=x.id`)
	if err != nil {
		return job, false, err
	}
	err = r.pool.QueryRow(ctx, `WITH candidate AS(SELECT id FROM user_outbox_events WHERE event_type='user.deleted.v1' AND attempts<8 AND
 ((status='pending' AND available_at<=clock_timestamp()) OR (status='running' AND locked_at<clock_timestamp()-interval '1 minute'))
 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE user_outbox_events e SET status='running',attempts=attempts+1,claim_token=gen_random_uuid(),locked_at=clock_timestamp(),updated_at=clock_timestamp()
 FROM candidate c WHERE e.id=c.id RETURNING e.id,e.aggregate_id,e.payload,e.attempts,e.claim_token`).Scan(&job.ID, &job.UserID, &job.Payload, &job.Attempts, &job.ClaimToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, false, nil
	}

	return job, err == nil, err
}

func (r *UserRepository) FinishDeletion(ctx context.Context, job DeletionJob, success bool) error {
	state, code := "published", ""
	if !success {
		state, code = privacyPending, "publication_failed"
		if job.Attempts >= 8 {
			state = privacyFailed
		}
	}
	delay := time.Duration(1<<min(job.Attempts, 8)) * time.Second
	tag, err := r.pool.Exec(ctx, `UPDATE user_outbox_events SET status=$3,last_error=$4,available_at=clock_timestamp()+$5::interval,locked_at=NULL,claim_token=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND claim_token=$2 AND status='running' AND locked_at>clock_timestamp()-interval '1 minute'`, job.ID, job.ClaimToken, state, code, delay.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrPrivacyLeaseLost
	}

	return nil
}
