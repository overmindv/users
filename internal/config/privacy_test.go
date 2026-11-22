package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/privacyowner"
)

func TestPrivacyDisabledAndOwnerValidation(t *testing.T) {
	t.Setenv("USERS_PRIVACY_ENABLED", "")
	t.Setenv("USERS_PRIVACY_OWNERS", "")
	cfg, err := loadPrivacy()
	if err != nil || cfg.Enabled {
		t.Fatal("privacy enabled by default", err)
	}
	t.Setenv("USERS_PRIVACY_ENABLED", "true")
	if _, err = loadPrivacy(); err == nil {
		t.Fatal("missing owners accepted")
	}
	owners := map[string]privacyowner.Endpoint{}
	for _, owner := range domain.PrivacyOwners {
		owners[owner] = privacyowner.Endpoint{URL: "https://" + owner + ".example.org", Token: owner + strings.Repeat("x", 32)}
	}
	write := func() {
		data, marshalErr := json.Marshal(owners)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Setenv("USERS_PRIVACY_OWNERS", string(data))
	}
	write()
	cfg, err = loadPrivacy()
	if err != nil || !cfg.Enabled {
		t.Fatal("valid privacy config rejected", err)
	}
	owners["tasks"] = privacyowner.Endpoint{URL: "http://untrusted.example.org", Token: strings.Repeat("a", 32)}
	write()
	if _, err = loadPrivacy(); err == nil {
		t.Fatal("cleartext remote endpoint accepted")
	}
	owners["tasks"] = owners["content"]
	write()
	if _, err = loadPrivacy(); err == nil {
		t.Fatal("shared owner token accepted")
	}
}
