package config

import "testing"

func TestLoginThrottleConfig(t *testing.T) {
	t.Setenv("MEDIA_USERS_TOKEN", "test")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoginThrottle.MaxAttempts != 5 || cfg.LoginThrottle.IPMaxAttempts != 100 {
		t.Fatalf("defaults: %+v", cfg.LoginThrottle)
	}
	for _, tc := range []struct{ key, value string }{
		{"LOGIN_MAX_ATTEMPTS", "0"}, {"LOGIN_MAX_ATTEMPTS", "garbage"}, {"LOGIN_MAX_ATTEMPTS", "10001"},
		{"LOGIN_IP_MAX_ATTEMPTS", "-1"}, {"LOGIN_IP_MAX_ATTEMPTS", "100001"},
		{"LOGIN_THROTTLE_WINDOW", "0s"}, {"LOGIN_THROTTLE_WINDOW", "25h"}, {"LOGIN_THROTTLE_WINDOW", "wrong"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid throttle config accepted")
			}
		})
	}
}

func TestReviewGrantConfig(t *testing.T) {
	t.Setenv("MEDIA_USERS_TOKEN", "media-test")
	t.Setenv("JWT_SECRET", "jwt-fixture-with-sufficient-length")
	t.Setenv("USERS_ENTITIES_URL", "http://entities:8080")
	for _, invalid := range []string{"", "short", "jwt-fixture-with-sufficient-length"} {
		t.Setenv("USERS_REVIEW_GRANTS_TOKEN", invalid)
		if _, err := Load(); err == nil {
			t.Error("invalid review grant token accepted")
		}
	}
	t.Setenv("USERS_REVIEW_GRANTS_TOKEN", "read-fixture-with-sufficient-length")
	if cfg, err := Load(); err != nil || cfg.ReviewGrants.EntitiesURL != "http://entities:8080" {
		t.Errorf("review grant config: %v", err)
	}
}

func TestNonLocalBootstrapRejected(t *testing.T) {
	t.Setenv("MEDIA_USERS_TOKEN", "test")
	t.Setenv("USERS_PROFILE", "production")
	t.Setenv("JWT_SECRET", "production-secret")
	t.Setenv("BOOTSTRAP_SUPERUSER_EMAIL", "admin@example.com")
	t.Setenv("BOOTSTRAP_SUPERUSER_PASSWORD", "unsafe-bootstrap")
	if _, err := Load(); err == nil {
		t.Fatal("production bootstrap accepted")
	}
}
