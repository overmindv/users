package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RecoveryMail struct {
	ID, ClaimToken string
	Payload        []byte
}
type RecoveryOutbox struct{ pool *pgxpool.Pool }

func NewRecoveryOutbox(pool *pgxpool.Pool) *RecoveryOutbox { return &RecoveryOutbox{pool: pool} }

func (o *RecoveryOutbox) Claim(ctx context.Context) (RecoveryMail, bool, error) {
	mail := RecoveryMail{ClaimToken: uuid.NewString()}
	err := o.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT m.id FROM recovery_mail_outbox m JOIN user_recovery_tokens t ON t.token_hash=m.token_hash
		WHERE m.sent_at IS NULL AND NOT m.exhausted AND m.attempts<8 AND m.next_attempt_at<=clock_timestamp()
		AND (m.lease_until IS NULL OR m.lease_until<=clock_timestamp()) AND t.used_at IS NULL AND t.expires_at>clock_timestamp()
		ORDER BY m.next_attempt_at,m.id LIMIT 1 FOR UPDATE OF m SKIP LOCKED
	) UPDATE recovery_mail_outbox m SET claim_token=$1,lease_until=clock_timestamp()+interval '1 minute',attempts=m.attempts+1
	FROM candidate c WHERE m.id=c.id RETURNING m.id,m.payload`, mail.ClaimToken).Scan(&mail.ID, &mail.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveryMail{}, false, nil
	}

	return mail, err == nil, err
}

func (o *RecoveryOutbox) Complete(ctx context.Context, mail RecoveryMail) error {
	_, err := o.pool.Exec(ctx, `UPDATE recovery_mail_outbox SET sent_at=clock_timestamp(),payload=''::bytea,claim_token=NULL,lease_until=NULL
		WHERE id=$1 AND claim_token=$2 AND lease_until>clock_timestamp()`, mail.ID, mail.ClaimToken)

	return err
}

func (o *RecoveryOutbox) Retry(ctx context.Context, mail RecoveryMail) error {
	_, err := o.pool.Exec(ctx, `UPDATE recovery_mail_outbox SET exhausted=(attempts>=8),
		next_attempt_at=clock_timestamp()+LEAST(900,5*power(2,attempts)) * interval '1 second',claim_token=NULL,lease_until=NULL
		WHERE id=$1 AND claim_token=$2 AND lease_until>clock_timestamp()`, mail.ID, mail.ClaimToken)

	return err
}

func (o *RecoveryOutbox) Cleanup(ctx context.Context) error {
	_, err := o.pool.Exec(ctx, `WITH expired AS (
		SELECT token_hash FROM user_recovery_tokens WHERE expires_at<=clock_timestamp() OR used_at IS NOT NULL
		ORDER BY expires_at LIMIT 1000 FOR UPDATE SKIP LOCKED
	) DELETE FROM user_recovery_tokens t USING expired e WHERE t.token_hash=e.token_hash`)
	if err != nil {
		return err
	}
	_, err = o.pool.Exec(ctx, `UPDATE recovery_mail_outbox SET exhausted=true,payload=''::bytea WHERE id IN (
		SELECT id FROM recovery_mail_outbox WHERE attempts>=8 AND NOT exhausted AND sent_at IS NULL
		AND lease_until<=clock_timestamp() LIMIT 1000 FOR UPDATE SKIP LOCKED)`)

	return err
}
