package postgres

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/users/internal/domain"
)

const (
	privacyDelete    = "delete"
	privacyPending   = "pending"
	privacyCompleted = "completed"
	privacyFailed    = "failed"
)

func (r *UserRepository) PreparePrivacy(ctx context.Context, in domain.PrivacyPreparation) (domain.PrivacyRequest, error) {
	tx, err := r.securityTx(ctx)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var valid bool
	err = tx.QueryRow(ctx, `SELECT password_hash=$2 AND security_version=$3 AND deleted_at IS NULL FROM users WHERE id=$1 FOR UPDATE`, in.UserID, in.PasswordHash, in.SecurityVersion).Scan(&valid)
	if err != nil || !valid {
		return domain.PrivacyRequest{}, domain.ErrUnauthorized
	}
	if err = privacySession(ctx, tx, in.UserID, in.SessionID, in.SecurityVersion); err != nil {
		return domain.PrivacyRequest{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM privacy_requests WHERE user_id=$1 AND created_at>clock_timestamp()-interval '1 day'`, in.UserID).Scan(&count); err != nil {
		return domain.PrivacyRequest{}, err
	}
	if count >= 5 {
		return domain.PrivacyRequest{}, domain.ErrTooManyRequests
	}
	if _, err = tx.Exec(ctx, `UPDATE privacy_requests SET state='expired' WHERE user_id=$1 AND kind=$2 AND state='awaiting_confirmation'`, in.UserID, in.Kind); err != nil {
		return domain.PrivacyRequest{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO privacy_requests(id,user_id,session_id,security_version,kind,receipt_hash) VALUES($1,$2,$3,$4,$5,$6)`, in.ID, in.UserID, in.SessionID, in.SecurityVersion, in.Kind, in.ReceiptHash)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.PrivacyRequest{}, err
	}

	return r.PrivacyStatus(ctx, in.ID, in.ReceiptHash)
}

func privacySession(ctx context.Context, tx pgx.Tx, userID, sessionID string, version int64) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT true FROM user_sessions WHERE id=$1 AND user_id=$2 AND security_version=$3 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE`, sessionID, userID, version).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUnauthorized
	}

	return err
}

func (r *UserRepository) ConfirmPrivacy(ctx context.Context, id string, proof []byte) (domain.PrivacyRequest, error) {
	var userID string
	if err := r.pool.QueryRow(ctx, `SELECT user_id FROM privacy_requests WHERE id=$1`, id).Scan(&userID); err != nil {
		return domain.PrivacyRequest{}, domain.ErrPrivacyRequest
	}
	tx, err := r.securityTx(domain.WithAuditActor(ctx, userID))
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	var deleted *time.Time
	if err = tx.QueryRow(ctx, `SELECT security_version,deleted_at FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&version, &deleted); err != nil {
		return domain.PrivacyRequest{}, err
	}
	var kind, state, session string
	var expected int64
	var digest []byte
	var confirmUntil, expires time.Time
	var confirmed *time.Time
	err = tx.QueryRow(ctx, `SELECT kind,state,session_id,security_version,receipt_hash,confirm_until,expires_at,confirmed_at FROM privacy_requests WHERE id=$1 FOR UPDATE`, id).Scan(&kind, &state, &session, &expected, &digest, &confirmUntil, &expires, &confirmed)
	if err != nil || len(proof) != 32 || subtle.ConstantTimeCompare(proof, digest) != 1 || !expires.After(time.Now()) {
		return domain.PrivacyRequest{}, domain.ErrPrivacyRequest
	}
	if confirmed != nil {
		if err = tx.Commit(ctx); err != nil {
			return domain.PrivacyRequest{}, err
		}

		return r.PrivacyStatus(ctx, id, proof)
	}
	if state != "awaiting_confirmation" || !confirmUntil.After(time.Now()) || version != expected || deleted != nil {
		return domain.PrivacyRequest{}, domain.ErrPrivacyRequest
	}
	if err = privacySession(ctx, tx, userID, session, version); err != nil {
		return domain.PrivacyRequest{}, err
	}
	if kind == privacyDelete {
		err = confirmDeletion(ctx, tx, id, userID)
	} else {
		err = confirmExport(ctx, tx, id, userID)
	}
	if err != nil {
		return domain.PrivacyRequest{}, mapError(err)
	}
	for _, owner := range domain.PrivacyOwners {
		if _, err = tx.Exec(ctx, `INSERT INTO privacy_owner_steps(request_id,owner) VALUES($1,$2)`, id, owner); err != nil {
			return domain.PrivacyRequest{}, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE privacy_requests SET state='processing',confirmed_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return domain.PrivacyRequest{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.PrivacyRequest{}, err
	}

	return r.PrivacyStatus(ctx, id, proof)
}

func confirmExport(ctx context.Context, tx pgx.Tx, id, userID string) error {
	_, err := tx.Exec(ctx, `UPDATE privacy_requests SET profile=(SELECT jsonb_build_object(
 'id',id,'email',email,'username',username,'first_name',first_name,'last_name',last_name,
 'birth_date',birth_date,'phone',phone,'avatar_file_id',avatar_file_id,'timezone',timezone,
 'locale',locale,'public_name',public_name,'leaderboard_visible',leaderboard_visible,'created_at',created_at)
 FROM users WHERE id=$2) WHERE id=$1`, id, userID)

	return err
}

func confirmDeletion(ctx context.Context, tx pgx.Tx, id, userID string) error {
	var version int64
	err := tx.QueryRow(ctx, `UPDATE users SET deleted_at=clock_timestamp(),updated_at=clock_timestamp(),
 email='deleted+'||id::text||'@example.invalid',username='deleted_'||substr(md5(id::text),1,24),
 password_hash='deleted',first_name='',last_name='',birth_date=NULL,phone=NULL,avatar_file_id=NULL,
 roles='{}',is_superuser=false,public_name=false,leaderboard_visible=false,email_verified=false,
 timezone='UTC',locale='ru-RU' WHERE id=$1 RETURNING security_version`, userID).Scan(&version)
	if err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE user_sessions SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE user_id=$1`,
		`UPDATE user_refresh_families SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE user_id=$1`,
		`DELETE FROM user_refresh_tokens WHERE family_id IN (SELECT id FROM user_refresh_families WHERE user_id=$1)`,
		`DELETE FROM user_recovery_tokens WHERE user_id=$1`,
		`DELETE FROM topic_review_grants WHERE user_id=$1`,
		`UPDATE privacy_requests SET state='expired',profile=NULL WHERE user_id=$1 AND kind='export'`,
		`DELETE FROM privacy_export_pages WHERE request_id IN (SELECT id FROM privacy_requests WHERE user_id=$1 AND kind='export')`,
	} {
		if _, err = tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	event := domain.DeletionEvent{EventID: id, EventType: "user.deleted.v1", SchemaVersion: 1, AggregateID: userID, AggregateVersion: version, OccurredAt: time.Now().UTC(), Payload: domain.DeletionPayload{UserID: userID, RequestID: id, Tombstone: true}}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var position int64
	if err = tx.QueryRow(ctx, `UPDATE privacy_deletion_counter SET position=position+1 WHERE id=true RETURNING position`).Scan(&position); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO privacy_deletion_ledger(position,request_id,user_id,event) VALUES($1,$2,$3,$4)`, position, id, userID, payload); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_outbox_events(id,aggregate_id,event_type,payload) VALUES($1,$2,'user.deleted.v1',$3)`, id, userID, payload)

	return err
}
