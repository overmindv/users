package postgres

import "context"

// Reconcile обходит не более 100 пользователей; failed события требуют явного replay.
func (r *AvatarOutbox) Reconcile(ctx context.Context, after string) (string, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text FROM users WHERE ($1='' OR id>NULLIF($1,'')::uuid) ORDER BY id LIMIT 100`, after)
	if err != nil {
		return after, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()

			return after, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, err
	}
	for _, id := range ids {
		// Блокировка Users сериализует reconciliation со сменой avatar и ручным replay.
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return after, err
		}
		_, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id)
		if err == nil {
			_, err = tx.Exec(ctx, `SELECT users_replay_avatar(u.id) FROM users u WHERE u.id=$1 AND NOT EXISTS (
   SELECT 1 FROM user_outbox_events e WHERE e.aggregate_id=u.id AND e.event_type='user.avatar_changed' AND e.payload->>'version'=u.avatar_version::text)`, id)
		}
		if err != nil {
			_ = tx.Rollback(ctx)

			return after, err
		}
		if err = tx.Commit(ctx); err != nil {
			return after, err
		}
		after = id
	}
	if len(ids) < 100 {
		return "", nil
	}

	return after, nil
}

func (r *AvatarOutbox) Exhausted(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM user_outbox_events WHERE event_type='user.avatar_changed' AND status='failed'`).Scan(&count)

	return count, err
}
