package domain

import (
	"testing"
	"time"
)

func TestLearnerSettings(t *testing.T) {
	t.Parallel()
	const utc = "UTC"
	const english = "en-US"
	user, err := NewUser(NewUserParams{ID: "id", Email: "a@example.org", Username: "alice", PasswordHash: "hash", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if user.Timezone() != utc || user.Locale() != "ru-RU" || user.PublicName() || user.LeaderboardVisible() {
		t.Fatal("unsafe defaults")
	}
	zone, locale, yes := "Europe/Madrid", "en-us", true
	if err := user.UpdateProfile(ProfilePatch{Timezone: &zone, Locale: &locale, PublicName: &yes, LeaderboardVisible: &yes}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if user.Timezone() != zone || user.Locale() != english || !user.PublicName() || !user.LeaderboardVisible() {
		t.Fatal("settings not applied")
	}
	for _, invalid := range []string{"Local", "", "../UTC", "Mars/Base"} {
		if err := user.UpdateProfile(ProfilePatch{Timezone: &invalid}, time.Now()); err == nil {
			t.Fatalf("accepted zone %q", invalid)
		}
	}
	for _, invalid := range []string{"", "invalid_@@", "en/US"} {
		if err := user.UpdateProfile(ProfilePatch{Locale: &invalid}, time.Now()); err == nil {
			t.Fatalf("accepted locale %q", invalid)
		}
	}
	if user.Timezone() != zone || user.Locale() != english {
		t.Fatal("invalid patch mutated settings")
	}
}
