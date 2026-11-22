package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/users/internal/domain"
)

// CreateSession сериализуется с изменениями безопасности и отвергает stale login snapshot.
func (r *UserRepository) CreateSession(ctx context.Context, user *domain.User, id string, expires time.Time) (int64, error) {
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
	_, err = tx.Exec(ctx, `INSERT INTO user_sessions(id,user_id,security_version,expires_at) VALUES($1,$2,$3,$4)`, id, user.ID(), version, expires)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}

	return version, nil
}

// CheckSession получает свежие роли только для действующей версии сессии.
func (r *UserRepository) CheckSession(ctx context.Context, userID, id string, version int64) ([]string, error) {
	var roles []string
	err := r.pool.QueryRow(ctx, `SELECT u.roles FROM users u JOIN user_sessions s ON s.user_id=u.id WHERE u.id=$1 AND s.id=$2 AND s.security_version=$3 AND u.security_version=s.security_version AND u.deleted_at IS NULL AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND ($4 OR (u.id::text NOT LIKE '00000000-0000-0000-0000-0000000000%' AND u.password_hash LIKE '$argon2id$%'))`, userID, id, version, r.localCredentials).Scan(&roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUnauthorized
	}

	return roles, err
}

// RevokeSession сохраняет отзыв; повторный logout безопасен.
func (r *UserRepository) RevokeSession(ctx context.Context, userID, id string) error {
	var familyID string
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(refresh_family_id::text,'') FROM user_sessions WHERE id=$1 AND user_id=$2`, id, userID).Scan(&familyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	return r.revokeAccessFamily(ctx, userID, id, familyID)
}

// UpgradePassword не перезаписывает пароль, изменённый после проверки входа.
func (r *UserRepository) UpgradePassword(ctx context.Context, user *domain.User, previous string) error {

	tx, err := r.securityTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `UPDATE users SET password_hash=$2,updated_at=$3 WHERE id=$1 AND deleted_at IS NULL AND password_hash=$4 AND security_version=$5 RETURNING security_version`, user.ID(), user.PasswordHash(), user.UpdatedAt(), previous, user.SecurityVersion()).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	user.SetSecurityVersion(version)

	return nil
}
