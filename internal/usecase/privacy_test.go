package usecase

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
)

type privacyStoreFake struct {
	user     *domain.User
	prepared bool
	digest   []byte
}

func (f *privacyStoreFake) GetByID(context.Context, string) (*domain.User, error) { return f.user, nil }
func (f *privacyStoreFake) PreparePrivacy(_ context.Context, in domain.PrivacyPreparation) (domain.PrivacyRequest, error) {
	f.prepared = true
	f.digest = in.ReceiptHash

	return domain.PrivacyRequest{ID: in.ID, Kind: in.Kind, State: "awaiting_confirmation"}, nil
}
func (f *privacyStoreFake) ConfirmPrivacy(_ context.Context, id string, hash []byte) (domain.PrivacyRequest, error) {
	if string(hash) != string(f.digest) {
		return domain.PrivacyRequest{}, domain.ErrUnauthorized
	}

	return domain.PrivacyRequest{ID: id, State: "processing"}, nil
}
func (f *privacyStoreFake) PrivacyStatus(context.Context, string, []byte) (domain.PrivacyRequest, error) {
	return domain.PrivacyRequest{}, nil
}

func TestPrivacyConfirmationProof(t *testing.T) {
	t.Parallel()
	user, err := domain.NewUser(domain.NewUserParams{ID: uuid.NewString(), Email: "owner@example.org", Username: "owner", PasswordHash: "correct", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	store := &privacyStoreFake{user: user}
	actor := domain.PrivacyActor{UserID: user.ID(), SessionID: uuid.NewString(), Version: 1}
	service := NewPrivacy(store, security.PlainTextHasher{})
	if _, err = service.Prepare(t.Context(), actor, "delete", "wrong"); !errors.Is(err, domain.ErrUnauthorized) || store.prepared {
		t.Fatal("password bypass", err)
	}
	start, err := service.Prepare(t.Context(), actor, "delete", "correct")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(start.Receipt))
	if len(start.Receipt) != 43 || string(store.digest) != string(digest[:]) || string(store.digest) == start.Receipt {
		t.Fatal("receipt not hashed")
	}
	if _, err = service.Confirm(t.Context(), start.Request.ID, start.Receipt); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Confirm(t.Context(), start.Request.ID, "short"); err == nil {
		t.Fatal("invalid proof accepted")
	}
	if _, err = service.Prepare(t.Context(), actor, "unknown", "correct"); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestPrivacyBlocksLegacyDelete(t *testing.T) {
	t.Parallel()
	service := &UserService{}
	service.SetPrivacyEnabled(true)
	if err := service.Delete(t.Context(), uuid.NewString()); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatal("legacy deletion bypassed confirmation", err)
	}
}
