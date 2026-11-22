//go:build component

package integration

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/parker"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

func TestLearnerSettingsPostgres(t *testing.T) {
	pool, err := parker.OpenPool(t.Context(), os.Getenv("COMPONENT_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := postgresrepo.NewUserRepository(pool)
	suffix := uuid.NewString()[:8]
	user, err := domain.NewUser(domain.NewUserParams{ID: uuid.NewString(), Email: domain.Email(suffix + "@example.org"), Username: domain.Username("u_" + suffix), PasswordHash: "hash", FirstName: "Secret" + suffix, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	found, err := repo.ListPublic(t.Context(), user.FirstName(), 10, 0)
	if err != nil || len(found) != 0 {
		t.Fatal("private name searchable", err)
	}
	yes, zone, locale := true, "Asia/Tokyo", "ja-JP"
	patch := domain.ProfilePatch{PublicName: &yes, LeaderboardVisible: &yes, Timezone: &zone, Locale: &locale}
	if err := user.UpdateProfile(patch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(t.Context(), user, patch); err != nil {
		t.Fatal(err)
	}
	restored, err := repo.GetByID(t.Context(), user.ID())
	if err != nil || restored.Timezone() != zone || restored.Locale() != locale || !restored.PublicName() || !restored.LeaderboardVisible() {
		t.Fatal("settings not persisted", err)
	}
	found, err = repo.ListPublic(t.Context(), user.FirstName(), 10, 0)
	if err != nil || len(found) != 1 {
		t.Fatal("public name not searchable", err)
	}
	no := false
	privatePatch := domain.ProfilePatch{PublicName: &no, LeaderboardVisible: &no}
	if err := restored.UpdateProfile(privatePatch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(t.Context(), restored, privatePatch); err != nil {
		t.Fatal(err)
	}
	// Старый снимок public=true не должен отменить opt-out другим изменением.
	if err := repo.Update(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	restored, err = repo.GetByID(t.Context(), user.ID())
	if err != nil || restored.PublicName() || restored.LeaderboardVisible() {
		t.Fatal("stale profile update undid privacy", err)
	}
	found, err = repo.ListPublic(t.Context(), user.FirstName(), 10, 0)
	if err != nil || len(found) != 0 {
		t.Fatal("private name searchable after opt-out", err)
	}
}
