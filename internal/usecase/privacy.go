package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

type PrivacyStore interface {
	GetByID(context.Context, string) (*domain.User, error)
	PreparePrivacy(context.Context, domain.PrivacyPreparation) (domain.PrivacyRequest, error)
	ConfirmPrivacy(context.Context, string, []byte) (domain.PrivacyRequest, error)
	PrivacyStatus(context.Context, string, []byte) (domain.PrivacyRequest, error)
}

type Privacy struct {
	store     PrivacyStore
	passwords PasswordHasher
}

func NewPrivacy(store PrivacyStore, passwords PasswordHasher) *Privacy {
	return &Privacy{store: store, passwords: passwords}
}

// Prepare требует актуальный пароль; repository повторно проверяет версию под lock.
func (s *Privacy) Prepare(ctx context.Context, actor domain.PrivacyActor, kind, password string) (domain.PrivacyStart, error) {
	if uuid.Validate(actor.UserID) != nil || uuid.Validate(actor.SessionID) != nil || actor.Version < 1 {
		return domain.PrivacyStart{}, domain.ErrUnauthorized
	}
	if (kind != "delete" && kind != "export") || len(password) < 1 || len(password) > 1024 {
		return domain.PrivacyStart{}, domain.ErrPrivacyRequest
	}
	user, err := s.store.GetByID(ctx, actor.UserID)
	if err != nil {
		return domain.PrivacyStart{}, err
	}
	if err = s.passwords.Compare(user.PasswordHash(), password); err != nil {
		return domain.PrivacyStart{}, domain.ErrUnauthorized
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return domain.PrivacyStart{}, err
	}
	receipt := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(receipt))
	request, err := s.store.PreparePrivacy(ctx, domain.PrivacyPreparation{ID: uuid.NewString(), UserID: actor.UserID, SessionID: actor.SessionID, Kind: kind, PasswordHash: user.PasswordHash(), SecurityVersion: actor.Version, ReceiptHash: digest[:]})
	if err != nil {
		return domain.PrivacyStart{}, err
	}

	return domain.PrivacyStart{Request: request, Receipt: receipt}, nil
}

func (s *Privacy) Confirm(ctx context.Context, id, receipt string) (domain.PrivacyRequest, error) {
	digest, err := privacyProof(id, receipt)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}

	return s.store.ConfirmPrivacy(ctx, id, digest)
}

func (s *Privacy) Status(ctx context.Context, id, receipt string) (domain.PrivacyRequest, error) {
	digest, err := privacyProof(id, receipt)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}

	return s.store.PrivacyStatus(ctx, id, digest)
}

func privacyProof(id, receipt string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(receipt)
	if uuid.Validate(id) != nil || err != nil || len(raw) != 32 || len(receipt) != 43 {
		return nil, domain.ErrPrivacyRequest
	}
	digest := sha256.Sum256([]byte(receipt))

	return digest[:], nil
}
