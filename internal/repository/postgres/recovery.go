package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/users/internal/domain"
)

func (r *UserRepository) QueueRecovery(ctx context.Context, email, purpose string, hash, sealed []byte) error {
	if len(hash) != 32 || len(sealed) == 0 || (purpose != domain.RecoveryReset && purpose != domain.RecoveryVerify) {
		return domain.ErrInvalidRecoveryToken
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	var version int64
	var verified bool
	err = tx.QueryRow(ctx, `SELECT id,security_version,email_verified FROM users WHERE email=$1 AND deleted_at IS NULL
		AND ($2 OR id::text NOT LIKE '00000000-0000-0000-0000-0000000000%') FOR UPDATE`, email, r.localCredentials).Scan(&id, &version, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if purpose == domain.RecoveryVerify && verified {
		return nil
	}
	// Только последнее письмо данного назначения остаётся действительным.
	if _, err = tx.Exec(ctx, `UPDATE user_recovery_tokens SET used_at=clock_timestamp() WHERE user_id=$1 AND purpose=$2 AND used_at IS NULL`, id, purpose); err != nil {
		return err
	}
	minutes := 30
	if purpose == domain.RecoveryVerify {
		minutes = 1440
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_recovery_tokens(token_hash,user_id,email,security_version,purpose,expires_at)
		VALUES($1,$2,$3,$4,$5,clock_timestamp()+$6*interval '1 minute')`, hash, id, email, version, purpose, minutes); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO recovery_mail_outbox(token_hash,payload) VALUES($1,$2)`, hash, sealed); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *UserRepository) ConsumeRecovery(ctx context.Context, hash []byte, purpose, passwordHash string) error {
	if len(hash) != 32 || (purpose != domain.RecoveryReset && purpose != domain.RecoveryVerify) || (purpose == domain.RecoveryReset && passwordHash == "") {
		return domain.ErrInvalidRecoveryToken
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, "SELECT user_id FROM user_recovery_tokens WHERE token_hash=$1", hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInvalidRecoveryToken
	}
	if err != nil {
		return err
	}
	// Тот же порядок блокировок, что у login/refresh: сначала account, затем token.
	var email string
	var version int64
	err = tx.QueryRow(ctx, "SELECT email,security_version FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id).Scan(&email, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInvalidRecoveryToken
	}
	if err != nil {
		return err
	}
	var consumed string
	err = tx.QueryRow(ctx, `UPDATE user_recovery_tokens SET used_at=clock_timestamp()
		WHERE token_hash=$1 AND purpose=$2 AND email=$3 AND security_version=$4 AND used_at IS NULL AND expires_at>clock_timestamp()
		RETURNING user_id`, hash, purpose, email, version).Scan(&consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInvalidRecoveryToken
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.actor_id',$1,true)", id); err != nil {
		return err
	}
	if purpose == domain.RecoveryReset {
		// Триггер users_security_version также блокирует stale login snapshots.
		if _, err = tx.Exec(ctx, "UPDATE users SET password_hash=$2,updated_at=clock_timestamp() WHERE id=$1", id, passwordHash); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE user_sessions SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE user_id=$1", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE user_refresh_families SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE user_id=$1", id); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(ctx, "UPDATE users SET email_verified=true,updated_at=clock_timestamp() WHERE id=$1", id); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
