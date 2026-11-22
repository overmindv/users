package usecase

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
)

type recoveryStoreFake struct {
	hash     []byte
	sealed   []byte
	purpose  string
	consumed bool
	err      error
}

func (f *recoveryStoreFake) QueueRecovery(_ context.Context, _, purpose string, hash, sealed []byte) error {
	f.hash, f.sealed, f.purpose = hash, sealed, purpose

	return f.err
}
func (f *recoveryStoreFake) ConsumeRecovery(_ context.Context, hash []byte, purpose, _ string) error {
	if f.consumed || string(hash) != string(f.hash) || purpose != f.purpose {
		return domain.ErrInvalidRecoveryToken
	}
	f.consumed = true

	return f.err
}

type recoveryCipherFake struct {
	message domain.RecoveryMessage
	err     error
}

func (f *recoveryCipherFake) Seal(message domain.RecoveryMessage) ([]byte, error) {
	f.message = message

	return []byte("encrypted"), f.err
}

type recoveryThrottleFake struct {
	allowed bool
	err     error
}

func (f recoveryThrottleFake) Check(context.Context, string, string) (bool, error) {
	return f.allowed, f.err
}

func TestRecoveryOneTimeAndNoTokenResponse(t *testing.T) {
	t.Parallel()
	store, cipher := &recoveryStoreFake{}, &recoveryCipherFake{}
	service := NewRecovery(store, cipher, security.PlainTextHasher{}, recoveryThrottleFake{allowed: true}, recoveryThrottleFake{allowed: true})
	ctx := context.Background()
	if err := service.Request(ctx, "Learner@example.com", domain.RecoveryReset, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if len(cipher.message.Token) != 43 || cipher.message.To != "learner@example.com" {
		t.Fatal("invalid token/email")
	}
	hash := sha256.Sum256([]byte(cipher.message.Token))
	if string(store.hash) != string(hash[:]) || string(store.sealed) != "encrypted" {
		t.Fatal("raw token persisted")
	}
	if err := service.Reset(ctx, cipher.message.Token, "new-password", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := service.Reset(ctx, cipher.message.Token, "new-password", "127.0.0.1"); !errors.Is(err, domain.ErrInvalidRecoveryToken) {
		t.Fatal(err)
	}
}

func TestRecoveryGuards(t *testing.T) {
	t.Parallel()
	for _, purpose := range []string{domain.RecoveryReset, domain.RecoveryVerify} {
		store, cipher := &recoveryStoreFake{}, &recoveryCipherFake{}
		service := NewRecovery(store, cipher, security.PlainTextHasher{}, recoveryThrottleFake{}, recoveryThrottleFake{allowed: true})
		if err := service.Request(context.Background(), "a@example.com", purpose, "unknown"); err != nil || store.hash != nil {
			t.Fatal("rate limit reveals request", err)
		}
	}
	store, cipher := &recoveryStoreFake{}, &recoveryCipherFake{}
	service := NewRecovery(store, cipher, security.PlainTextHasher{}, recoveryThrottleFake{allowed: true}, recoveryThrottleFake{allowed: true})
	ctx := context.Background()
	if err := service.Request(ctx, "a@example.com", "wrong", "unknown"); err == nil {
		t.Fatal("wrong purpose")
	}
	if err := service.Request(ctx, "bad-email", domain.RecoveryReset, "unknown"); err == nil {
		t.Fatal("bad email")
	}
	if err := service.Request(ctx, "a@example.com", domain.RecoveryVerify, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(ctx, cipher.message.Token, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(ctx, cipher.message.Token, "unknown"); !errors.Is(err, domain.ErrInvalidRecoveryToken) {
		t.Fatal(err)
	}
	if err := service.Reset(ctx, "invalid", "new-password", "unknown"); err == nil {
		t.Fatal("malformed token")
	}
	if err := service.Reset(ctx, strings.Repeat("a", 43), strings.Repeat("x", 1025), "unknown"); err == nil {
		t.Fatal("oversized password")
	}
}
