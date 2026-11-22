package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/overmindv/users/internal/domain"
)

type refreshStoreStub struct {
	hash    []byte
	grant   domain.RefreshGrant
	err     error
	revoked bool
}

func (s *refreshStoreStub) CreateRefreshSession(_ context.Context, _ *domain.User, _, _ string, hash []byte, _, _ time.Time) (int64, error) {
	s.hash = hash

	return 1, s.err
}
func (s *refreshStoreStub) RotateRefreshSession(_ context.Context, hash, _ []byte, _ string, _ time.Time) (domain.RefreshGrant, error) {
	s.hash = hash

	return s.grant, s.err
}
func (s *refreshStoreStub) RevokeRefreshSession(_ context.Context, hash []byte) error {
	s.hash = hash
	s.revoked = true

	return s.err
}

func TestRefreshTokenLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	manager := NewManager("test-secret", "test", time.Hour)
	store := &refreshStoreStub{grant: domain.RefreshGrant{UserID: "user", Roles: []string{"student"}, SecurityVersion: 1, AccessExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour)}}
	manager.SetRefreshStore(store)
	user, err := domain.NewUser(domain.NewUserParams{ID: "user", Email: "u@example.com", Username: "testuser", PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.IssueRefreshSession(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.RefreshToken)
	if err != nil || len(raw) != 32 {
		t.Fatal("refresh entropy")
	}
	hash := sha256.Sum256([]byte(first.RefreshToken))
	if string(store.hash) != string(hash[:]) {
		t.Fatal("stored raw credential or wrong hash")
	}
	second, err := manager.RefreshSession(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.RefreshToken == first.RefreshToken || second.UserID != "user" {
		t.Fatal("refresh not rotated")
	}
	claims, err := manager.parseClaims(second.AccessToken)
	if err != nil || claims.SessionVersion != 1 || claims.ID == "" {
		t.Fatalf("claims: %v", err)
	}
	if err = manager.RevokeRefreshSession(ctx, second.RefreshToken); err != nil || !store.revoked {
		t.Fatalf("revoke: %v", err)
	}
	for _, bad := range []string{"", "short", first.RefreshToken + "=", first.RefreshToken + "extra"} {
		if _, err = manager.RefreshSession(ctx, bad); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("malformed: %v", err)
		}
	}
	store.err = errors.New("storage down")
	if _, err = manager.RefreshSession(ctx, first.RefreshToken); !errors.Is(err, store.err) {
		t.Fatalf("failed open: %v", err)
	}
}
