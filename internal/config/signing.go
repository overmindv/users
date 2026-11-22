package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/overmindv/users/internal/auth"
)

func configureSigning(c *Config) error {
	seedText, kid := os.Getenv("JWT_SIGNING_SEED"), os.Getenv("JWT_SIGNING_KID")
	overlapText, audience, legacyText := os.Getenv("JWT_OVERLAP_PUBLIC_KEYS"), os.Getenv("JWT_AUDIENCE"), os.Getenv("JWT_LEGACY_HS256_UNTIL")
	c.LoginContextSecret = os.Getenv("USERS_LOGIN_CONTEXT_SECRET")
	if seedText == "" && kid == "" && overlapText == "" && audience == "" && legacyText == "" {
		if c.LoginContextSecret == "" {
			c.LoginContextSecret = c.JWT.Secret
		}

		return nil
	}
	if len(seedText) > 128 || len(overlapText) > 16384 {
		return errors.New("signing configuration too large")
	}
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil {
		return errors.New("invalid signing seed")
	}
	var overlap map[string]string
	if overlapText != "" {
		if err = json.Unmarshal([]byte(overlapText), &overlap); err != nil {
			return errors.New("invalid overlap public keys")
		}
	}
	c.JWT.SigningKeys, err = auth.NewSigningKeys(seed, kid, overlap)
	if err != nil {
		return err
	}
	c.JWT.Secret = os.Getenv("JWT_SECRET")
	c.JWT.Audience = audience
	if c.JWT.Audience == "" {
		c.JWT.Audience = "overmindv-gateway"
	}
	c.JWT.Lifetime = 15 * time.Minute
	if raw := os.Getenv("JWT_TTL"); raw != "" {
		c.JWT.Lifetime, err = time.ParseDuration(raw)
		if err != nil {
			return errors.New("invalid JWT_TTL")
		}
	}
	c.JWT.LegacyUntil = time.Time{}
	if legacyText != "" {
		c.JWT.LegacyUntil, err = time.Parse(time.RFC3339, legacyText)
		if err != nil {
			return errors.New("invalid legacy JWT deadline")
		}
	}
	if len(c.LoginContextSecret) < 32 || len(c.LoginContextSecret) > 4096 || strings.ContainsAny(c.LoginContextSecret, " \r\n\t") || c.LoginContextSecret == c.JWT.Secret {
		return errors.New("asymmetric JWT requires a separate USERS_LOGIN_CONTEXT_SECRET")
	}
	_, err = auth.NewSigningManager(c.JWT.SigningKeys, c.JWT.Issuer, c.JWT.Audience, c.JWT.Lifetime, c.JWT.Secret, c.JWT.LegacyUntil)

	return err
}
