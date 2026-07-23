package domain

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

var (
	phonePattern    = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.]{2,31}$`)
)

// Email хранит нормализованный email пользователя.
type Email string

// NewEmail создаёт value object email с проверкой формата и длины.
func NewEmail(value string) (Email, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || len(normalized) > 254 {
		return "", ErrInvalidEmail
	}

	return Email(normalized), nil
}

// String возвращает email как строку.
func (e Email) String() string { return string(e) }

// Phone хранит телефон пользователя в международном формате.
type Phone string

// NewPhone создаёт value object телефона и разрешает пустое значение для необязательного поля профиля.
func NewPhone(value string) (Phone, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", nil
	}

	if !phonePattern.MatchString(normalized) {
		return "", ErrInvalidPhone
	}

	return Phone(normalized), nil
}

// String возвращает телефон как строку.
func (p Phone) String() string { return string(p) }

// Username хранит публичное имя пользователя.
type Username string

// NewUsername создаёт value object username с приведением к нижнему регистру и проверкой допустимых символов.
func NewUsername(value string) (Username, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !usernamePattern.MatchString(normalized) {
		return "", fmt.Errorf("%w: use 3-32 lowercase letters, digits, dot or underscore", ErrInvalidUsername)
	}

	return Username(normalized), nil
}

// String возвращает username как строку.
func (u Username) String() string { return string(u) }
