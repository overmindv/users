package graphql

import (
	"time"

	"github.com/overmindv/arcee/internal/delivery/graphql/model"
	"github.com/overmindv/arcee/internal/domain"
	"github.com/overmindv/arcee/internal/usecase"
)

func parseBirthDate(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}

	parsed, err := time.Parse(time.DateOnly, *value)
	if err != nil {
		return nil, domain.ErrInvalidBirthDate
	}

	return &parsed, nil
}

func toUser(user *domain.User) *model.User {
	var birthDate, phone *string
	if value := user.BirthDate(); value != nil {
		formatted := value.Format(time.DateOnly)
		birthDate = &formatted
	}

	if value := user.Phone().String(); value != "" {
		phone = &value
	}

	return &model.User{
		ID:        user.ID(),
		Email:     user.Email().String(),
		Username:  user.Username().String(),
		FirstName: user.FirstName(),
		LastName:  user.LastName(),
		BirthDate: birthDate,
		Phone:     phone,
		CreatedAt: user.CreatedAt().Format(time.RFC3339),
		UpdatedAt: user.UpdatedAt().Format(time.RFC3339),
	}
}

func toAuthPayload(result *usecase.AuthResult) *model.AuthPayload {
	return &model.AuthPayload{
		User:      toUser(result.User),
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt.Format(time.RFC3339),
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}

	return *value
}
