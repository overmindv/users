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
