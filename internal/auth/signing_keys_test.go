package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/overmindv/users/internal/domain"
)

func TestEd25519Rotation(t *testing.T) {
	t.Parallel()
	seed1, seed2 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	keys1, err := NewSigningKeys(seed1, "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := NewSigningManager(keys1, "users", "gateway", 15*time.Minute, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	oldToken, _, err := old.Issue("user", nil)
	if err != nil {
		t.Fatal(err)
	}
	pub1 := ed25519.NewKeyFromSeed(seed1).Public().(ed25519.PublicKey)
	keys2, err := NewSigningKeys(seed2, "two", map[string]string{"one": base64.StdEncoding.EncodeToString(pub1)})
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewSigningManager(keys2, "users", "gateway", 15*time.Minute, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = current.Parse(oldToken); err != nil {
		t.Fatal("overlap rejected", err)
	}
	newToken, _, err := current.Issue("user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Parse(newToken); err == nil {
		t.Fatal("unknown kid accepted")
	}
	other, err := NewSigningManager(keys2, "users", "wrong-audience", 15*time.Minute, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Parse(newToken); err == nil {
		t.Fatal("wrong audience accepted")
	}
	w := httptest.NewRecorder()
	current.JWKS().ServeHTTP(w, httptest.NewRequest("GET", "/.well-known/jwks.json", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kid":"one"`) || !strings.Contains(w.Body.String(), `"kid":"two"`) || strings.Contains(w.Body.String(), `"d":`) {
		t.Fatal("invalid public JWKS", w.Body.String())
	}
}

func TestEd25519LegacyDeadline(t *testing.T) {
	t.Parallel()
	now := time.Now()
	keys, err := NewSigningKeys(bytes.Repeat([]byte{3}, 32), "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewSigningManager(keys, "users", "gateway", time.Minute, "legacy", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "user", Issuer: "users", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}).SignedString([]byte("legacy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Parse(token); err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err = manager.Parse(token); err == nil {
		t.Fatal("legacy accepted after deadline")
	}
}

type signingSessionStore struct{}

func (signingSessionStore) CreateSession(context.Context, *domain.User, string, time.Time) (int64, error) {
	return 1, nil
}
func (signingSessionStore) CheckSession(context.Context, string, string, int64) ([]string, error) {
	return []string{"student"}, nil
}
func (signingSessionStore) RevokeSession(context.Context, string, string) error { return nil }

func TestEd25519SessionAndRefresh(t *testing.T) {
	t.Parallel()
	keys, err := NewSigningKeys(bytes.Repeat([]byte{4}, 32), "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewSigningManager(keys, "users", "gateway", 15*time.Minute, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	m.SetSessionStore(signingSessionStore{})
	user, err := domain.NewUser(domain.NewUserParams{ID: "user", Email: "u@example.com", Username: "testuser", PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := m.IssueUser(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Parse(access); err != nil {
		t.Fatal("durable access", err)
	}
	m.SetRefreshStore(&refreshStoreStub{grant: domain.RefreshGrant{UserID: "user", SecurityVersion: 1, AccessExpiresAt: time.Now().Add(10 * time.Minute), RefreshExpiresAt: time.Now().Add(time.Hour)}})
	first, err := m.IssueRefreshSession(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Parse(first.AccessToken); err != nil {
		t.Fatal("first refresh access", err)
	}
	second, err := m.RefreshSession(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Parse(second.AccessToken); err != nil {
		t.Fatal("rotated access", err)
	}
}
