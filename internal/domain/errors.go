package domain

import "errors"

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPhone       = errors.New("invalid phone")
	ErrInvalidUsername    = errors.New("invalid username")
	ErrInvalidName        = errors.New("invalid name")
	ErrInvalidBirthDate   = errors.New("invalid birth date")
	ErrInvalidPassword    = errors.New("password must contain at least 8 characters")
	ErrInvalidUserID      = errors.New("invalid user id")
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrUsernameExists     = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
)
