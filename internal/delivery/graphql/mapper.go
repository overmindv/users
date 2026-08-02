package graphql

import (
	"time"

	"github.com/overmindv/users/internal/delivery/graphql/model"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/usecase"
)

// parseBirthDate переводит GraphQL date string в domain date.
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

// toUser преобразует доменного пользователя в GraphQL model.
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
		ID:          user.ID(),
		Email:       user.Email().String(),
		Username:    user.Username().String(),
		FirstName:   user.FirstName(),
		LastName:    user.LastName(),
		BirthDate:   birthDate,
		Phone:       phone,
		Roles:       user.Roles(),
		IsAdmin:     user.IsAdmin(),
		IsSuperuser: user.IsSuperuser(),
		CreatedAt:   user.CreatedAt().Format(time.RFC3339),
		UpdatedAt:   user.UpdatedAt().Format(time.RFC3339),
	}
}

// toAuthPayload преобразует результат usecase в GraphQL auth payload.
func toAuthPayload(result *usecase.AuthResult) *model.AuthPayload {
	return &model.AuthPayload{
		User:      toUser(result.User),
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt.Format(time.RFC3339),
	}
}

// stringValue возвращает строку из nullable GraphQL поля.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// boolValue возвращает bool из nullable GraphQL поля.
func boolValue(value *bool) bool {
	return value != nil && *value
}

// intValue возвращает int из nullable GraphQL поля или fallback.
func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}

	return *value
}
