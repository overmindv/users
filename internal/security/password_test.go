package security

import (
	"strings"
	"testing"
	"time"
)

func TestArgon2IDHasherRoundTrip(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2IDHasher()
	hash, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash missing argon2id prefix: %q", hash)
	}
	if hasher.RequiresUpgrade(hash) {
		t.Fatal("argon2id hash should not require upgrade")
	}

	if err := hasher.Compare(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("compare correct password: %v", err)
	}
	if err := hasher.Compare(hash, "wrong-password"); err == nil {
		t.Fatal("compare wrong password should fail")
	}
}

func TestArgon2IDHasherUniqueSalts(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2IDHasher()
	a, err := hasher.Hash("same-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hasher.Hash("same-password")
	if err != nil {
		t.Fatal(err)
	}

	if a == b {
		t.Fatal("expected distinct salts to produce different hashes")
	}
}

func TestArgon2IDHasherLegacyPlaintext(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2IDHasher()
	if !hasher.RequiresUpgrade("plain-password") {
		t.Fatal("legacy plaintext must require upgrade")
	}
	// Legacy открытый текст сравнивается до одноразовой миграции.
	if err := hasher.Compare("plain-password", "plain-password"); err != nil {
		t.Fatalf("legacy compare correct: %v", err)
	}
	if err := hasher.Compare("plain-password", "other"); err == nil {
		t.Fatal("legacy compare wrong password should fail")
	}
}

func TestArgon2IDHasherRejectsMalformed(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2IDHasher()
	for _, malformed := range []string{
		"",
		"$argon2id$v=19",                      // неполный
		"$argon2id$v=19$m=64,t=2,p=2$!!!$!!!", // некорректный base64
		"$bcrypt$12$somesalt$somehash",        // чужой алгоритм
	} {
		if err := hasher.Compare(malformed, "password"); err == nil {
			t.Fatalf("expected compare to reject malformed hash %q", malformed)
		}
	}
}

func TestMemoryLoginThrottlerAllowsUpToLimit(t *testing.T) {
	t.Parallel()

	throttler := NewMemoryLoginThrottler(3, time.Minute)
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	if !throttler.Allow("user@example.com", now) {
		t.Fatal("first attempt should be allowed")
	}
	if !throttler.Allow("user@example.com", now) {
		t.Fatal("second attempt should be allowed")
	}
	if !throttler.Allow("user@example.com", now) {
		t.Fatal("third attempt should be allowed")
	}
	if throttler.Allow("user@example.com", now) {
		t.Fatal("fourth attempt should be blocked")
	}
	// Другие ключи не затрагиваются.
	if !throttler.Allow("other@example.com", now) {
		t.Fatal("independent key should not be blocked")
	}
}

func TestMemoryLoginThrottlerWindowResets(t *testing.T) {
	t.Parallel()

	throttler := NewMemoryLoginThrottler(2, time.Minute)
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	throttler.Allow("user@example.com", start)
	throttler.Allow("user@example.com", start)
	if throttler.Allow("user@example.com", start) {
		t.Fatal("expected block within window")
	}

	// После истечения окна счётчик сбрасывается.
	if !throttler.Allow("user@example.com", start.Add(2*time.Minute)) {
		t.Fatal("expected window to reset after expiry")
	}
}

func TestMemoryLoginThrottlerReset(t *testing.T) {
	t.Parallel()

	throttler := NewMemoryLoginThrottler(2, time.Minute)
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	throttler.Allow("user@example.com", now)
	throttler.Allow("user@example.com", now)
	if throttler.Allow("user@example.com", now) {
		t.Fatal("expected block")
	}

	throttler.Reset("user@example.com")
	if !throttler.Allow("user@example.com", now) {
		t.Fatal("expected reset to allow attempts again")
	}
}
