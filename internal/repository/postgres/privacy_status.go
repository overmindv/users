package postgres

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

func (r *UserRepository) PrivacyStatus(ctx context.Context, id string, proof []byte) (domain.PrivacyRequest, error) {
	var result domain.PrivacyRequest
	var digest []byte
	var live bool
	err := r.pool.QueryRow(ctx, `SELECT id,kind,CASE WHEN state='awaiting_confirmation' AND confirm_until<=clock_timestamp() THEN 'expired' ELSE state END,created_at,expires_at,receipt_hash,expires_at>clock_timestamp(),
 CASE WHEN kind='delete' THEN COALESCE((SELECT status FROM user_outbox_events WHERE id=privacy_requests.id),'not_queued') ELSE '' END FROM privacy_requests WHERE id=$1`, id).Scan(&result.ID, &result.Kind, &result.State, &result.CreatedAt, &result.ExpiresAt, &digest, &live, &result.Publication)
	if err != nil || !live || len(proof) != 32 || subtle.ConstantTimeCompare(digest, proof) != 1 {
		return domain.PrivacyRequest{}, domain.ErrPrivacyRequest
	}
	result.Owners = []domain.PrivacyOwnerStatus{}
	rows, err := r.pool.Query(ctx, `SELECT owner,state,error_code FROM privacy_owner_steps WHERE request_id=$1 ORDER BY owner`, id)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner domain.PrivacyOwnerStatus
		if err = rows.Scan(&owner.Owner, &owner.State, &owner.ErrorCode); err != nil {
			return domain.PrivacyRequest{}, err
		}
		result.Owners = append(result.Owners, owner)
	}

	return result, rows.Err()
}

// AcknowledgePrivacy получает owner только из проверенного service token.
func (r *UserRepository) AcknowledgePrivacy(ctx context.Context, id, owner, state, code string) error {
	if uuid.Validate(id) != nil || !slices.Contains(domain.PrivacyOwners, owner) || (state != privacyCompleted && state != privacyFailed) || (code != "" && code != "processing_failed" && code != "policy_pending") {
		return domain.ErrPrivacyRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var kind string
	if err = tx.QueryRow(ctx, `SELECT kind FROM privacy_requests WHERE id=$1 AND confirmed_at IS NOT NULL FOR UPDATE`, id).Scan(&kind); err != nil || kind != privacyDelete {
		return domain.ErrPrivacyRequest
	}
	if state == privacyCompleted {
		code = ""
	}
	if _, err = tx.Exec(ctx, `UPDATE privacy_owner_steps SET state=$3,error_code=$4 WHERE request_id=$1 AND owner=$2 AND state<>'completed'`, id, owner, state, code); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE privacy_requests SET state=CASE WHEN NOT EXISTS(SELECT 1 FROM privacy_owner_steps WHERE request_id=$1 AND state<>'completed') THEN 'completed' ELSE 'processing' END WHERE id=$1`, id); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type DeletionLedgerPage struct {
	Watermark int64             `json:"watermark"`
	NextAfter int64             `json:"next_after"`
	Events    []json.RawMessage `json:"events"`
}

// DeletionLedger не удаляется по TTL: он защищает от восстановления старых данных.
func (r *UserRepository) DeletionLedger(ctx context.Context, after, watermark int64, limit int) (DeletionLedgerPage, error) {
	if after < 0 || watermark < 0 || limit < 1 || limit > 100 {
		return DeletionLedgerPage{}, domain.ErrPrivacyRequest
	}
	var current int64
	if err := r.pool.QueryRow(ctx, `SELECT position FROM privacy_deletion_counter WHERE id=true`).Scan(&current); err != nil {
		return DeletionLedgerPage{}, err
	}
	if watermark == 0 {
		watermark = current
	}
	if watermark > current {
		return DeletionLedgerPage{}, domain.ErrPrivacyRequest
	}
	if after > watermark {
		return DeletionLedgerPage{}, domain.ErrPrivacyRequest
	}
	result := DeletionLedgerPage{Watermark: watermark, NextAfter: watermark, Events: []json.RawMessage{}}
	rows, err := r.pool.Query(ctx, `SELECT position,event FROM privacy_deletion_ledger WHERE position>$1 AND position<=$2 ORDER BY position LIMIT $3`, after, watermark, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var last int64
	for rows.Next() {
		var position int64
		var event []byte
		if err = rows.Scan(&position, &event); err != nil {
			return result, err
		}
		if len(result.Events) == limit {
			result.NextAfter = last

			break
		}
		result.Events = append(result.Events, json.RawMessage(event))
		last = position
	}

	return result, rows.Err()
}
