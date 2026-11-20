package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/overmindv/users/internal/domain"
)

func (r *UserRepository) ChangeReviewGrant(ctx context.Context, input domain.ReviewGrantChange) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		const admin = `
			SELECT 1 FROM users AS u
			JOIN user_sessions AS s ON s.user_id = u.id
			WHERE u.id = $1 AND s.id = $2 AND s.security_version = $3
			  AND u.security_version = s.security_version AND u.deleted_at IS NULL
			  AND s.revoked_at IS NULL AND s.expires_at > clock_timestamp()
			  AND (u.is_superuser OR 'admin' = ANY(u.roles) OR 'superuser' = ANY(u.roles))
			FOR SHARE OF u, s`
		var found int
		if err := tx.QueryRow(ctx, admin, input.ActorID, input.SessionID, input.SessionVersion).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrPermissionDenied
			}

			return err
		}
		if input.Grant {
			const target = `SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL FOR SHARE`
			if err := tx.QueryRow(ctx, target, input.UserID).Scan(&found); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return domain.ErrUserNotFound
				}

				return err
			}
			_, err := tx.Exec(ctx, grantTopicReview, input.TopicID, input.UserID, input.ActorID, input.Reason)

			return err
		}
		_, err := tx.Exec(ctx, revokeTopicReview, input.TopicID, input.UserID, input.ActorID, input.Reason)

		return err
	})
}

const grantTopicReview = `
	WITH changed AS (
	    INSERT INTO topic_review_grants (topic_id, user_id) VALUES ($1, $2)
	    ON CONFLICT DO NOTHING RETURNING topic_id, user_id
	)
	INSERT INTO user_security_audit (actor_id, subject_id, action, details)
	SELECT $3, user_id, 'topic_review_granted', jsonb_build_object('topic_id', topic_id, 'reason', $4::text)
	FROM changed`

const revokeTopicReview = `
	WITH changed AS (
	    DELETE FROM topic_review_grants WHERE topic_id = $1 AND user_id = $2 RETURNING topic_id, user_id
	)
	INSERT INTO user_security_audit (actor_id, subject_id, action, details)
	SELECT $3, user_id, 'topic_review_revoked', jsonb_build_object('topic_id', topic_id, 'reason', $4::text)
	FROM changed`

const reviewAccountUsable = `u.deleted_at IS NULL
	AND ($3::boolean OR (u.id::text NOT LIKE '00000000-0000-0000-0000-0000000000%' AND u.password_hash LIKE '$argon2id$%'))`

func (r *UserRepository) ListReviewGrants(ctx context.Context, topicID, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	// Проверяем аккаунт по ID после перехода к курсору в индексе прав.
	const query = `
		SELECT g.user_id FROM topic_review_grants AS g
		WHERE g.topic_id = $1 AND g.user_id > $2
		  AND (SELECT ` + reviewAccountUsable + ` FROM users AS u WHERE u.id = g.user_id)
		ORDER BY g.user_id LIMIT $4`
	rows, err := r.pool.Query(ctx, query, topicID, after, r.localCredentials, limit)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (r *UserRepository) CheckReviewGrants(ctx context.Context, topicID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	const query = `
		SELECT g.user_id FROM topic_review_grants AS g JOIN users AS u ON u.id = g.user_id
		WHERE g.topic_id = $1 AND g.user_id = ANY($2::uuid[]) AND ` + reviewAccountUsable + `
		ORDER BY g.user_id`
	rows, err := r.pool.Query(ctx, query, topicID, ids, r.localCredentials)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
