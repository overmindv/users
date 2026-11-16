package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/overmindv/users/internal/domain"
)

func (r *UserRepository) securityTx(ctx context.Context) (pgx.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.actor_id',$1,true)`, domain.AuditActor(ctx)); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (r *UserRepository) securityExec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx, err := r.securityTx(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return tag, mapError(err)
	}
	return tag, tx.Commit(ctx)
}
