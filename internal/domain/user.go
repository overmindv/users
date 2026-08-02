package domain

import (
	"strings"
	"time"
)

// User представляет aggregate пользователя Users с профилем, ролями и soft delete состоянием.
type User struct {
	id           string
	email        Email
	passwordHash string
	username     Username
	firstName    string
	lastName     string
	birthDate    *time.Time
	phone        Phone
	roles        []string
	isSuperuser  bool
	createdAt    time.Time
	updatedAt    time.Time
	deletedAt    *time.Time
}

// RoleAdmin задаёт каноническое имя роли администратора.
const RoleAdmin = "admin"

// NewUserParams описывает входные данные создания нового пользователя.
type NewUserParams struct {
	ID           string
	Email        Email
	PasswordHash string
	Username     Username
	FirstName    string
	LastName     string
	BirthDate    *time.Time
	Phone        Phone
	Roles        []string
	IsSuperuser  bool
	Now          time.Time
}

// RestoreUserParams описывает данные восстановления пользователя из хранилища.
type RestoreUserParams struct {
	NewUserParams
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// ProfilePatch описывает частичное изменение профиля пользователя.
type ProfilePatch struct {
	Username  *Username
	FirstName *string
	LastName  *string
	BirthDate **time.Time
	Phone     *Phone
}

// NewUser создаёт нового пользователя из валидированных параметров.
// На вход получает обязательные поля аккаунта и текущее время, на выход возвращает доменную модель или ошибку валидации.
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
		roles:        normalizeRoles(params.Roles, params.IsSuperuser),
		isSuperuser:  params.IsSuperuser,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

// RestoreUser восстанавливает пользователя из состояния хранилища.
// На вход получает все сохранённые поля, на выход возвращает доменную модель без повторной бизнес-валидации.
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
		roles:        normalizeRoles(params.Roles, params.IsSuperuser),
		isSuperuser:  params.IsSuperuser,
		createdAt:    params.CreatedAt,
		updatedAt:    params.UpdatedAt,
		deletedAt:    cloneTime(params.DeletedAt),
	}
}

// UpdateProfile применяет частичное обновление профиля пользователя.
// На вход получает patch и текущее время, на выход возвращает ошибку, если новые значения нарушают правила профиля.
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

// SoftDelete помечает пользователя удалённым.
// На вход получает текущее время, на выходе меняет deleted_at и updated_at в доменной модели.
func (u *User) SoftDelete(now time.Time) {
	deleted := now.UTC()
	u.deletedAt = &deleted
	u.updatedAt = deleted
}

// SetAdmin включает или выключает роль администратора.
// На вход получает желаемое состояние и текущее время, на выход возвращает ошибку при попытке снять admin у суперпользователя.
func (u *User) SetAdmin(enabled bool, now time.Time) error {
	if u.isSuperuser && !enabled {
		return ErrCannotDemoteSuperuser
	}

	if enabled {
		u.roles = addRole(u.roles, RoleAdmin)
	} else {
		u.roles = removeRole(u.roles, RoleAdmin)
	}
	u.updatedAt = now.UTC()

	return nil
}

// PromoteSuperuser назначает пользователю статус суперпользователя.
// На вход получает текущее время, на выходе гарантирует роль admin и обновляет updated_at.
func (u *User) PromoteSuperuser(now time.Time) {
	u.isSuperuser = true
	u.roles = addRole(u.roles, RoleAdmin)
	u.updatedAt = now.UTC()
}

// normalizeName нормализует имя или фамилию пользователя.
// На вход получает строку, на выход возвращает trimmed-значение или ошибку ограничения длины.
func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 100 {
		return "", ErrInvalidName
	}

	return value, nil
}

// validateBirthDate проверяет дату рождения относительно текущего времени.
// На вход получает дату и now, на выход возвращает ошибку, если дата находится в будущем.
func validateBirthDate(value *time.Time, now time.Time) error {
	if value != nil && value.After(now) {
		return ErrInvalidBirthDate
	}

	return nil
}

// cloneTime копирует указатель на время.
// На вход получает nullable time pointer, на выход возвращает независимую копию или nil.
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

// normalizeRoles приводит роли к каноническому набору.
// На вход получает исходный список и признак суперпользователя, на выход возвращает роли без дублей и пустых значений.
func normalizeRoles(values []string, isSuperuser bool) []string {
	roles := make([]string, 0, len(values)+1)
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		roles = addRole(roles, value)
	}
	if isSuperuser {
		roles = addRole(roles, RoleAdmin)
	}

	return roles
}

// addRole добавляет роль в список без дублирования.
// На вход получает текущие роли и новую роль, на выход возвращает обновлённый список.
func addRole(values []string, role string) []string {
	for _, value := range values {
		if value == role {
			return values
		}
	}

	return append(values, role)
}

// removeRole удаляет роль из списка.
// На вход получает текущие роли и роль для удаления, на выход возвращает новый список без этой роли.
func removeRole(values []string, role string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != role {
			filtered = append(filtered, value)
		}
	}

	return filtered
}

// ID возвращает идентификатор пользователя.
func (u *User) ID() string { return u.id }

// Email возвращает email пользователя.
func (u *User) Email() Email { return u.email }

// PasswordHash возвращает сохранённый hash пароля.
func (u *User) PasswordHash() string { return u.passwordHash }

// Username возвращает username пользователя.
func (u *User) Username() Username { return u.username }

// FirstName возвращает имя пользователя.
func (u *User) FirstName() string { return u.firstName }

// LastName возвращает фамилию пользователя.
func (u *User) LastName() string { return u.lastName }

// BirthDate возвращает копию даты рождения пользователя.
func (u *User) BirthDate() *time.Time { return cloneTime(u.birthDate) }

// Phone возвращает телефон пользователя.
func (u *User) Phone() Phone { return u.phone }

// Roles возвращает копию ролей пользователя.
// На выходе всегда отдаётся slice, который можно безопасно передать в PostgreSQL array encoder.
func (u *User) Roles() []string {
	roles := make([]string, len(u.roles))
	copy(roles, u.roles)

	return roles
}

// IsSuperuser возвращает признак bootstrap-суперпользователя.
func (u *User) IsSuperuser() bool { return u.isSuperuser }

// CreatedAt возвращает время создания пользователя.
func (u *User) CreatedAt() time.Time { return u.createdAt }

// UpdatedAt возвращает время последнего изменения пользователя.
func (u *User) UpdatedAt() time.Time { return u.updatedAt }

// DeletedAt возвращает копию времени soft delete.
func (u *User) DeletedAt() *time.Time { return cloneTime(u.deletedAt) }

// IsAdmin проверяет административные права пользователя.
// На выходе возвращает true для суперпользователя или пользователя с ролью admin.
func (u *User) IsAdmin() bool {
	if u.isSuperuser {
		return true
	}
	for _, role := range u.roles {
		if role == RoleAdmin {
			return true
		}
	}

	return false
}
