package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/arcee/internal/domain"
)

const userColumns = `id, email, password_hash, username, first_name, last_name, birth_date, phone, roles, is_superuser, created_at, updated_at, deleted_at`

// UserRepository хранит пользователей в PostgreSQL.
type UserRepository struct{ pool *pgxpool.Pool }

// NewUserRepository создаёт PostgreSQL repository поверх готового pgx pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create вставляет нового активного пользователя.
func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, username, first_name, last_name, birth_date, phone, roles, is_superuser, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11, $12)`,
		user.ID(), user.Email().String(), user.PasswordHash(), user.Username().String(), user.FirstName(), user.LastName(),
		user.BirthDate(), user.Phone().String(), user.Roles(), user.IsSuperuser(), user.CreatedAt(), user.UpdatedAt())

	return mapError(err)
}

// GetByID возвращает активного пользователя по ID.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 AND deleted_at IS NULL`, id)

	return scanUser(row)
}

// GetByEmail возвращает активного пользователя по email.
func (r *UserRepository) GetByEmail(ctx context.Context, email domain.Email) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1 AND deleted_at IS NULL`, email.String())

	return scanUser(row)
}

// GetByUsername возвращает активного пользователя по username.
func (r *UserRepository) GetByUsername(ctx context.Context, username domain.Username) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1 AND deleted_at IS NULL`, username.String())

	return scanUser(row)
}

// List возвращает активных пользователей с поиском по email или username.
func (r *UserRepository) List(ctx context.Context, search string, limit, offset int) ([]*domain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+userColumns+`
		FROM users
		WHERE deleted_at IS NULL
		  AND ($1 = '' OR username ILIKE '%' || $1 || '%' OR email ILIKE '%' || $1 || '%')
		ORDER BY created_at, id
		LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	users := make([]*domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

// Update сохраняет изменяемые поля профиля пользователя.
func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE users SET username=$2, first_name=$3, last_name=$4, birth_date=$5, phone=NULLIF($6, ''), updated_at=$7
		WHERE id=$1 AND deleted_at IS NULL`, user.ID(), user.Username().String(), user.FirstName(), user.LastName(), user.BirthDate(), user.Phone().String(), user.UpdatedAt())

	if err = mapError(err); err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// UpdateRoles сохраняет роли пользователя и флаг суперпользователя.
func (r *UserRepository) UpdateRoles(ctx context.Context, user *domain.User) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE users SET roles=$2, is_superuser=$3, updated_at=$4
		WHERE id=$1 AND deleted_at IS NULL`, user.ID(), user.Roles(), user.IsSuperuser(), user.UpdatedAt())
	if err != nil {
		return fmt.Errorf("update user roles: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// SoftDelete сохраняет soft delete пользователя.
func (r *UserRepository) SoftDelete(ctx context.Context, user *domain.User) error {
	result, err := r.pool.Exec(ctx, `UPDATE users SET deleted_at=$2, updated_at=$3 WHERE id=$1 AND deleted_at IS NULL`, user.ID(), user.DeletedAt(), user.UpdatedAt())
	if err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// scanner описывает общий contract pgx.Row и pgx.Rows для scanUser.
type scanner interface{ Scan(...any) error }

// scanUser восстанавливает доменную модель пользователя из строки PostgreSQL.
func scanUser(row scanner) (*domain.User, error) {
	var id, emailValue, passwordHash, usernameValue, firstName, lastName string
	var birthDate, deletedAt *time.Time
	var phoneValue *string
	var roles []string
	var isSuperuser bool
	var createdAt, updatedAt time.Time

	if err := row.Scan(&id, &emailValue, &passwordHash, &usernameValue, &firstName, &lastName, &birthDate, &phoneValue, &roles, &isSuperuser, &createdAt, &updatedAt, &deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}

	email, err := domain.NewEmail(emailValue)
	if err != nil {
		return nil, fmt.Errorf("stored email: %w", err)
	}

	username, err := domain.NewUsername(usernameValue)
	if err != nil {
		return nil, fmt.Errorf("stored username: %w", err)
	}

	phone := domain.Phone("")
	if phoneValue != nil {
		phone, err = domain.NewPhone(*phoneValue)
		if err != nil {
			return nil, fmt.Errorf("stored phone: %w", err)
		}
	}

	// Повторно создаём value objects, чтобы повреждённые данные в БД не попали выше repository слоя.
	return domain.RestoreUser(domain.RestoreUserParams{
		NewUserParams: domain.NewUserParams{
			ID:           id,
			Email:        email,
			PasswordHash: passwordHash,
			Username:     username,
			FirstName:    firstName,
			LastName:     lastName,
			BirthDate:    birthDate,
			Phone:        phone,
			Roles:        roles,
			IsSuperuser:  isSuperuser,
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		DeletedAt: deletedAt,
	}), nil
}

// mapError переводит ошибки PostgreSQL constraint в доменные ошибки Arcee.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		// Имя constraint используется для стабильного выбора бизнес-ошибки при конфликте уникальности.
		switch postgresError.ConstraintName {
		case "users_email_active_key":
			return domain.ErrEmailAlreadyExists
		case "users_username_active_key":
			return domain.ErrUsernameExists
		}
	}

	return fmt.Errorf("postgres operation: %w", err)
}
