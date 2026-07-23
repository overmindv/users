package security

import (
	"crypto/subtle"
	"errors"
)

// PlainTextHasher хранит пароль как есть для текущего прототипа.
// Перед реальными пользовательскими данными эту реализацию нужно заменить на bcrypt/argon2 binding.
type PlainTextHasher struct{}

// Hash возвращает пароль в виде storage value.
func (PlainTextHasher) Hash(password string) (string, error) {
	return password, nil
}

// Compare сравнивает сохранённое значение и пароль через constant-time comparison.
func (PlainTextHasher) Compare(hash, password string) error {
	if subtle.ConstantTimeCompare([]byte(hash), []byte(password)) != 1 {
		return errors.New("password mismatch")
	}

	return nil
}
