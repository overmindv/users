package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/domain"
)

type sessionMemory struct {
	revoked bool
	fail    bool
}

func (s *sessionMemory) CreateSession(context.Context, *domain.User, string, time.Time) (int64, error) {
	return 1, nil
}
func (s *sessionMemory) CheckSession(context.Context, string, string, int64) ([]string, error) {
	if s.fail {
		return nil, errors.New("offline")
	}
	if s.revoked {
		return nil, domain.ErrUnauthorized
	}
	return []string{"user"}, nil
}
func (s *sessionMemory) RevokeSession(context.Context, string, string) error {
	s.revoked = true
	return nil
}

func TestPersistentSessionRevocation(t *testing.T) {
	m := NewManager("secret", "users", time.Hour)
	legacy, _, _ := m.Issue("user", nil)
	store := &sessionMemory{}
	m.SetSessionStore(store)
	u, _ := domain.NewUser(domain.NewUserParams{ID: "user", Email: domain.Email("a@example.com"), Username: domain.Username("alice"), PasswordHash: "hash", Now: time.Now()})
	token, _, err := m.IssueUser(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Validate(context.Background(), legacy); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("legacy accepted: %v", err)
	}
	c, err := m.Validate(context.Background(), token)
	if err != nil || c.ID == "" || c.SessionVersion != 1 {
		t.Fatalf("claims: %+v %v", c, err)
	}
	store.fail = true
	if _, err = m.Validate(context.Background(), token); err == nil {
		t.Fatal("outage accepted")
	}
	store.fail = false
	if err = m.Revoke(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Validate(context.Background(), token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("revoked accepted: %v", err)
	}
}

func TestSessionHTTP(t *testing.T) {
	m := NewManager("secret", "users", time.Hour)
	store := &sessionMemory{}
	m.SetSessionStore(store)
	u, _ := domain.NewUser(domain.NewUserParams{ID: "user", Email: domain.Email("a@example.com"), Username: domain.Username("alice"), PasswordHash: "hash", Now: time.Now()})
	token, _, err := m.IssueUser(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	call := func(revoke bool, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		m.SessionHTTP(revoke).ServeHTTP(w, r)
		return w
	}
	w := call(false, token)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "example.com") || !strings.Contains(w.Body.String(), "session_id") {
		t.Fatalf("check: %d %s", w.Code, w.Body.String())
	}
	store.fail = true
	if w = call(false, token); w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code)
	}
	store.fail = false
	if w = call(true, token); w.Code != http.StatusNoContent {
		t.Fatal(w.Code)
	}
	if w = call(false, token); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	if w = call(false, "invalid"); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
}
