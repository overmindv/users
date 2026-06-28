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

type Email string

func NewEmail(value string) (Email, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || len(normalized) > 254 {
		return "", ErrInvalidEmail
	}

	return Email(normalized), nil
}

func (e Email) String() string { return string(e) }

type Phone string

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

func (p Phone) String() string { return string(p) }

type Username string

func NewUsername(value string) (Username, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !usernamePattern.MatchString(normalized) {
		return "", fmt.Errorf("%w: use 3-32 lowercase letters, digits, dot or underscore", ErrInvalidUsername)
	}

	return Username(normalized), nil
}

func (u Username) String() string { return string(u) }
