package domain

import (
	"strings"
	"time"
)

type User struct {
	id           string
	email        Email
	passwordHash string
	username     Username
	firstName    string
	lastName     string
	birthDate    *time.Time
	phone        Phone
	createdAt    time.Time
	updatedAt    time.Time
	deletedAt    *time.Time
}

type NewUserParams struct {
	ID           string
	Email        Email
	PasswordHash string
	Username     Username
	FirstName    string
	LastName     string
	BirthDate    *time.Time
	Phone        Phone
	Now          time.Time
}

type RestoreUserParams struct {
	NewUserParams
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type ProfilePatch struct {
	Username  *Username
	FirstName *string
	LastName  *string
	BirthDate **time.Time
	Phone     *Phone
}

func NewUser(params NewUserParams) (*User, error) {
	if params.ID == "" {
		return nil, ErrInvalidUserID
	}

	if params.Email == "" {
		return nil, ErrInvalidEmail
	}

	if params.Username == "" {
		return nil, ErrInvalidUsername
	}

	if params.PasswordHash == "" {
		return nil, ErrInvalidPassword
	}

	firstName, err := normalizeName(params.FirstName)
	if err != nil {
		return nil, err
	}

	lastName, err := normalizeName(params.LastName)
	if err != nil {
		return nil, err
	}

	if err := validateBirthDate(params.BirthDate, params.Now); err != nil {
		return nil, err
	}

	now := params.Now.UTC()

	return &User{
		id:           params.ID,
		email:        params.Email,
		passwordHash: params.PasswordHash,
		username:     params.Username,
		firstName:    firstName,
		lastName:     lastName,
		birthDate:    cloneTime(params.BirthDate),
		phone:        params.Phone,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

func RestoreUser(params RestoreUserParams) *User {
	return &User{
		id:           params.ID,
		email:        params.Email,
		passwordHash: params.PasswordHash,
		username:     params.Username,
		firstName:    params.FirstName,
		lastName:     params.LastName,
		birthDate:    cloneTime(params.BirthDate),
		phone:        params.Phone,
		createdAt:    params.CreatedAt,
		updatedAt:    params.UpdatedAt,
		deletedAt:    cloneTime(params.DeletedAt),
	}
}

func (u *User) UpdateProfile(patch ProfilePatch, now time.Time) error {
	if patch.Username != nil {
		u.username = *patch.Username
	}

	if patch.FirstName != nil {
		value, err := normalizeName(*patch.FirstName)
		if err != nil {
			return err
		}
		u.firstName = value
	}

	if patch.LastName != nil {
		value, err := normalizeName(*patch.LastName)
		if err != nil {
			return err
		}
		u.lastName = value
	}

	if patch.BirthDate != nil {
		if err := validateBirthDate(*patch.BirthDate, now); err != nil {
			return err
		}
		u.birthDate = cloneTime(*patch.BirthDate)
	}

	if patch.Phone != nil {
		u.phone = *patch.Phone
	}
	u.updatedAt = now.UTC()

	return nil
}

func (u *User) SoftDelete(now time.Time) {
	deleted := now.UTC()
	u.deletedAt = &deleted
	u.updatedAt = deleted
}

func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 100 {
		return "", ErrInvalidName
	}

	return value, nil
}

func validateBirthDate(value *time.Time, now time.Time) error {
	if value != nil && value.After(now) {
		return ErrInvalidBirthDate
	}

	return nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

func (u *User) ID() string            { return u.id }
func (u *User) Email() Email          { return u.email }
func (u *User) PasswordHash() string  { return u.passwordHash }
func (u *User) Username() Username    { return u.username }
func (u *User) FirstName() string     { return u.firstName }
func (u *User) LastName() string      { return u.lastName }
func (u *User) BirthDate() *time.Time { return cloneTime(u.birthDate) }
func (u *User) Phone() Phone          { return u.phone }
func (u *User) CreatedAt() time.Time  { return u.createdAt }
func (u *User) UpdatedAt() time.Time  { return u.updatedAt }
func (u *User) DeletedAt() *time.Time { return cloneTime(u.deletedAt) }
