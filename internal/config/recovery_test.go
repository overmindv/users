package config

import (
	"encoding/base64"
	"testing"
)

func TestRecoveryConfiguration(t *testing.T) {
	for _, key := range []string{"USERS_RECOVERY_SMTP_ADDRESS", "USERS_RECOVERY_SMTP_USERNAME", "USERS_RECOVERY_SMTP_PASSWORD", "USERS_RECOVERY_FROM", "USERS_RECOVERY_MAIL_KEY"} {
		t.Setenv(key, "")
	}
	if c, err := loadRecovery(); err != nil || c.Enabled {
		t.Fatal(c.Enabled, err)
	}
	t.Setenv("USERS_RECOVERY_SMTP_ADDRESS", "smtp.example.com:587")
	if _, err := loadRecovery(); err == nil {
		t.Fatal("partial config accepted")
	}
	t.Setenv("USERS_RECOVERY_SMTP_USERNAME", "user")
	t.Setenv("USERS_RECOVERY_SMTP_PASSWORD", "secret")
	t.Setenv("USERS_RECOVERY_FROM", "noreply@example.com")
	t.Setenv("USERS_RECOVERY_MAIL_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if c, err := loadRecovery(); err != nil || !c.Enabled {
		t.Fatal(c.Enabled, err)
	}
	t.Setenv("USERS_RECOVERY_FROM", "noreply@example.com\r\nBcc: evil@example.com")
	if _, err := loadRecovery(); err == nil {
		t.Fatal("header injection accepted")
	}
}
