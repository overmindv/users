package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestSigningConfiguration(t *testing.T) {
	for _, key := range []string{"JWT_SIGNING_SEED", "JWT_SIGNING_KID", "JWT_OVERLAP_PUBLIC_KEYS", "JWT_AUDIENCE", "JWT_LEGACY_HS256_UNTIL", "JWT_SECRET", "JWT_TTL", "USERS_LOGIN_CONTEXT_SECRET"} {
		t.Setenv(key, "")
	}
	c := Config{JWT: JWT{Secret: "legacy", Issuer: "users"}}
	if err := configureSigning(&c); err != nil || c.LoginContextSecret != "legacy" {
		t.Fatal(err)
	}
	t.Setenv("JWT_SIGNING_SEED", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	t.Setenv("JWT_SIGNING_KID", "active")
	t.Setenv("USERS_LOGIN_CONTEXT_SECRET", strings.Repeat("c", 32))
	if err := configureSigning(&c); err != nil || c.JWT.SigningKeys == nil || c.JWT.Lifetime != 15*time.Minute || c.JWT.Secret != "" {
		t.Fatal("asymmetric config", err)
	}
	t.Setenv("JWT_TTL", "24h")
	if err := configureSigning(&c); err == nil {
		t.Fatal("long access TTL accepted")
	}
	t.Setenv("JWT_TTL", "15m")
	t.Setenv("JWT_SECRET", "legacy")
	if err := configureSigning(&c); err == nil {
		t.Fatal("legacy without deadline accepted")
	}
	t.Setenv("JWT_LEGACY_HS256_UNTIL", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	if err := configureSigning(&c); err != nil {
		t.Fatal(err)
	}
}
