package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/users/internal/domain"
)

// CreateRefreshSession атомарно сохраняет access-сессию и первый hash family.
func (r *UserRepository) CreateRefreshSession(ctx context.Context, user *domain.User, familyID, sessionID string, hash []byte, accessExpiry, refreshExpiry time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `SELECT security_version FROM users WHERE id=$1 AND deleted_at IS NULL AND password_hash=$2 AND roles=$3 AND is_superuser=$4 AND security_version=$5 FOR UPDATE`, user.ID(), user.PasswordHash(), user.Roles(), user.IsSuperuser(), user.SecurityVersion()).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrUnauthorized
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_refresh_families(id,user_id,security_version,expires_at) VALUES($1,$2,$3,$4)`, familyID, user.ID(), version, refreshExpiry); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_refresh_tokens(token_hash,family_id) VALUES($1,$2)`, hash, familyID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_sessions(id,user_id,security_version,expires_at,refresh_family_id) VALUES($1,$2,$3,LEAST($4::timestamptz,$5::timestamptz),$6)`, sessionID, user.ID(), version, accessExpiry, refreshExpiry, familyID); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}

	return version, nil
}

// lockRefresh соблюдает порядок users → family, общий для rotation и logout.
func (r *UserRepository) lockRefresh(ctx context.Context, tx pgx.Tx, hash []byte) (string, domain.RefreshGrant, bool, error) {
	var familyID, userID string
	err := tx.QueryRow(ctx, `SELECT f.id,f.user_id FROM user_refresh_tokens t JOIN user_refresh_families f ON f.id=t.family_id WHERE t.token_hash=$1`, hash).Scan(&familyID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.RefreshGrant{}, false, domain.ErrUnauthorized
	}
	if err != nil {
		return "", domain.RefreshGrant{}, false, err
	}
	grant := domain.RefreshGrant{UserID: userID}
	grant.User, err = scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 AND deleted_at IS NULL AND ($2 OR (id::text NOT LIKE '00000000-0000-0000-0000-0000000000%' AND password_hash LIKE '$argon2id$%')) FOR UPDATE`, userID, r.localCredentials))
	if errors.Is(err, domain.ErrUserNotFound) {
		return "", grant, false, domain.ErrUnauthorized
	}
	if err != nil {
		return "", grant, false, err
	}
	grant.Roles = grant.User.Roles()
	grant.SecurityVersion = grant.User.SecurityVersion()
	var version int64
	var revoked *time.Time
	err = tx.QueryRow(ctx, `SELECT security_version,expires_at,revoked_at FROM user_refresh_families WHERE id=$1 FOR UPDATE`, familyID).Scan(&version, &grant.RefreshExpiresAt, &revoked)
	if err != nil {
		return "", grant, false, err
	}
	// Время проверяем после ожидания блокировки, не по timestamp начала транзакции.
	var expired, used bool
	err = tx.QueryRow(ctx, `SELECT f.expires_at<=clock_timestamp(),t.used_at IS NOT NULL FROM user_refresh_families f JOIN user_refresh_tokens t ON t.family_id=f.id WHERE t.token_hash=$1`, hash).Scan(&expired, &used)
	if err != nil {
		return "", grant, false, err
	}
	if expired || revoked != nil || version != grant.SecurityVersion {
		return "", grant, false, domain.ErrUnauthorized
	}

	return familyID, grant, used, nil
}

// RotateRefreshSession потребляет token и создаёт преемника в той же транзакции.
// Reuse коммитит отзыв перед возвратом Unauthorized: rollback здесь был бы уязвимостью.
func (r *UserRepository) RotateRefreshSession(ctx context.Context, hash, nextHash []byte, sessionID string, accessExpiry time.Time) (domain.RefreshGrant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RefreshGrant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	familyID, grant, used, err := r.lockRefresh(ctx, tx, hash)
	if err != nil {
		return domain.RefreshGrant{}, err
	}
	if used {
		if err = revokeRefreshFamily(ctx, tx, familyID); err != nil {
			return domain.RefreshGrant{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.RefreshGrant{}, err
		}

		return domain.RefreshGrant{}, domain.ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE user_refresh_tokens SET used_at=clock_timestamp() WHERE token_hash=$1`, hash); err != nil {
		return domain.RefreshGrant{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_refresh_tokens(token_hash,family_id) VALUES($1,$2)`, nextHash, familyID); err != nil {
		return domain.RefreshGrant{}, err
	}
	grant.AccessExpiresAt = accessExpiry
	if grant.RefreshExpiresAt.Before(accessExpiry) {
		grant.AccessExpiresAt = grant.RefreshExpiresAt
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_sessions(id,user_id,security_version,expires_at,refresh_family_id) VALUES($1,$2,$3,$4,$5)`, sessionID, grant.UserID, grant.SecurityVersion, grant.AccessExpiresAt, familyID); err != nil {
		return domain.RefreshGrant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.RefreshGrant{}, err
	}

	return grant, nil
}

// revokeRefreshFamily отзывает family и все выданные из неё access JWT.
func revokeRefreshFamily(ctx context.Context, tx pgx.Tx, familyID string) error {
	if _, err := tx.Exec(ctx, `UPDATE user_refresh_families SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE id=$1`, familyID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE refresh_family_id=$1`, familyID)

	return err
}

// RevokeRefreshSession не раскрывает существование token и допускает повтор logout.
func (r *UserRepository) RevokeRefreshSession(ctx context.Context, hash []byte) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	familyID, _, _, err := r.lockRefresh(ctx, tx, hash)
	if errors.Is(err, domain.ErrUnauthorized) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = revokeRefreshFamily(ctx, tx, familyID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// revokeAccessFamily сериализует обычный logout с refresh того же устройства.
func (r *UserRepository) revokeAccessFamily(ctx context.Context, userID, sessionID, familyID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id); err != nil {
		return err
	}
	if familyID != "" {
		if err = revokeRefreshFamily(ctx, tx, familyID); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE id=$1 AND user_id=$2`, sessionID, userID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
