package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/overmindv/arcee/internal/domain"
)

func TestManagerIssueAndParse(t *testing.T) {
	manager := NewManager("secret", "arcee", 24*time.Hour)
	manager.now = func() time.Time {
		return time.Unix(1000, 0)
	}

	token, expires, err := manager.Issue("user-id")
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(time.Unix(1000, 0).Add(24 * time.Hour)) {
		t.Fatalf("unexpected expiry %v", expires)
	}

	userID, err := manager.Parse(token)
	if err != nil || userID != "user-id" {
		t.Fatalf("Parse() = %q, %v", userID, err)
	}

	manager.now = func() time.Time { return expires.Add(time.Second) }
	if _, err := manager.Parse(token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestBearerToken(t *testing.T) {
	if token, err := BearerToken("Bearer abc"); err != nil || token != "abc" {
		t.Fatalf("BearerToken() = %q, %v", token, err)
	}

	if _, err := BearerToken("abc"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}
