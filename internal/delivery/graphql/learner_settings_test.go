package graphql

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/domain"
)

func TestLearnerSettingsVisibility(t *testing.T) {
	t.Parallel()
	const first = "Private"
	user, err := domain.NewUser(domain.NewUserParams{ID: "id", Email: "secret@example.org", Username: "learner", PasswordHash: "hash", FirstName: first, LastName: "Person", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	owner := toUser(user)
	if owner.Timezone != "UTC" || owner.Locale != "ru-RU" || owner.FirstName != first || owner.Email != "secret@example.org" {
		t.Fatal("owner lost fields")
	}
	public := toPublicUser(user)
	payload, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if public.DisplayName != "learner" || public.FirstName != "" || public.LastName != "" || public.LeaderboardVisible {
		t.Fatal("unsafe public defaults")
	}
	for _, secret := range []string{"secret@example.org", first, "Person", "timezone", "locale"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("public payload contains %q", secret)
		}
	}
	yes := true
	if err := user.UpdateProfile(domain.ProfilePatch{PublicName: &yes, LeaderboardVisible: &yes}, time.Now()); err != nil {
		t.Fatal(err)
	}
	public = toPublicUser(user)
	if public.DisplayName != "Private Person" || public.FirstName != first || !public.LeaderboardVisible {
		t.Fatal("opt-in ignored")
	}
}
