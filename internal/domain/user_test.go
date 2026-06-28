package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValueObjects(t *testing.T) {
	t.Parallel()

	if email, err := NewEmail(" User@Example.COM "); err != nil || email.String() != "user@example.com" {
		t.Fatalf("NewEmail() = %q, %v", email, err)
	}

	if _, err := NewEmail("broken"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}

	if _, err := NewPhone("89991234567"); !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("expected ErrInvalidPhone, got %v", err)
	}

	if phone, err := NewPhone("+79991234567"); err != nil || phone.String() != "+79991234567" {
		t.Fatalf("NewPhone() = %q, %v", phone, err)
	}

	if phone, err := NewPhone(""); err != nil || phone.String() != "" {
		t.Fatalf("empty NewPhone() = %q, %v", phone, err)
	}

	if username, err := NewUsername(" Alice_1 "); err != nil || username.String() != "alice_1" {
		t.Fatalf("NewUsername() = %q, %v", username, err)
	}

	if _, err := NewUsername("a!"); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("expected ErrInvalidUsername, got %v", err)
	}
}

func TestRestoreAndAllProfileFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	birthDate := now.AddDate(-20, 0, 0)
	deletedAt := now.Add(time.Hour)
	email, _ := NewEmail("user@example.com")
	username, _ := NewUsername("alice")
	phone, _ := NewPhone("+79991234567")

	user := RestoreUser(RestoreUserParams{
		NewUserParams: NewUserParams{ID: "id", Email: email, PasswordHash: "password", Username: username, FirstName: "A", LastName: "B", BirthDate: &birthDate, Phone: phone},
		CreatedAt:     now, UpdatedAt: now, DeletedAt: &deletedAt,
	})
	if user.ID() != "id" || user.Email() != email || user.PasswordHash() != "password" || user.Username() != username || user.LastName() != "B" || user.BirthDate() == nil || user.CreatedAt() != now || user.UpdatedAt() != now || user.DeletedAt() == nil {
		t.Fatalf("restored user does not preserve state")
	}

	newUsername, _ := NewUsername("alice_new")
	first, last := "New", "Name"
	newBirthDate := birthDate.AddDate(1, 0, 0)
	newBirthDatePtr := &newBirthDate

	if err := user.UpdateProfile(ProfilePatch{Username: &newUsername, FirstName: &first, LastName: &last, BirthDate: &newBirthDatePtr, Phone: &phone}, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if user.Username() != newUsername || user.LastName() != last || !user.BirthDate().Equal(newBirthDate) {
		t.Fatal("all profile fields were not updated")
	}

	var cleared *time.Time
	if err := user.UpdateProfile(ProfilePatch{BirthDate: &cleared}, now.Add(3*time.Hour)); err != nil || user.BirthDate() != nil {
		t.Fatalf("clear birth date: %v", err)
	}
}

func TestUserRejectsInvalidNamesOnCreateAndUpdate(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	email, _ := NewEmail("user@example.com")
	username, _ := NewUsername("alice")
	longName := string(make([]rune, 101))

	if _, err := NewUser(NewUserParams{ID: "id", Email: email, Username: username, PasswordHash: "password", FirstName: longName, Now: now}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected invalid first name, got %v", err)
	}

	user, _ := NewUser(NewUserParams{ID: "id", Email: email, Username: username, PasswordHash: "password", Now: now})
	if err := user.UpdateProfile(ProfilePatch{LastName: &longName}, now); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected invalid last name, got %v", err)
	}

	future := now.Add(time.Hour)
	futurePtr := &future
	if err := user.UpdateProfile(ProfilePatch{BirthDate: &futurePtr}, now); !errors.Is(err, ErrInvalidBirthDate) {
		t.Fatalf("expected invalid birth date, got %v", err)
	}
}

func TestNewUserRequiresAggregateIdentity(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	email, _ := NewEmail("user@example.com")
	username, _ := NewUsername("alice")

	tests := []struct {
		name   string
		params NewUserParams
		want   error
	}{
		{"id", NewUserParams{Email: email, Username: username, PasswordHash: "password", Now: now}, ErrInvalidUserID},
		{"email", NewUserParams{ID: "id", Username: username, PasswordHash: "password", Now: now}, ErrInvalidEmail},
		{"username", NewUserParams{ID: "id", Email: email, PasswordHash: "password", Now: now}, ErrInvalidUsername},
		{"password", NewUserParams{ID: "id", Email: email, Username: username, Now: now}, ErrInvalidPassword},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewUser(test.params); !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestUserUpdateAndDelete(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	email, _ := NewEmail("user@example.com")
	username, _ := NewUsername("alice")

	user, err := NewUser(NewUserParams{ID: "id", Email: email, Username: username, PasswordHash: "password", Now: now})
	if err != nil {
		t.Fatal(err)
	}

	name := " Alice "
	phone, _ := NewPhone("+79991234567")
	if err := user.UpdateProfile(ProfilePatch{FirstName: &name, Phone: &phone}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if user.FirstName() != "Alice" || user.Phone() != phone {
		t.Fatalf("unexpected profile: %q %q", user.FirstName(), user.Phone())
	}

	user.SoftDelete(now.Add(2 * time.Hour))
	if user.DeletedAt() == nil {
		t.Fatal("expected soft deletion")
	}
}

func TestUserRejectsFutureBirthDate(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	email, _ := NewEmail("user@example.com")
	username, _ := NewUsername("alice")

	_, err := NewUser(NewUserParams{ID: "id", Email: email, Username: username, PasswordHash: "password", BirthDate: &future, Now: now})
	if !errors.Is(err, ErrInvalidBirthDate) {
		t.Fatalf("expected ErrInvalidBirthDate, got %v", err)
	}
}
