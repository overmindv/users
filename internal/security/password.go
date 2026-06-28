package security

import (
	"crypto/subtle"
	"errors"
)

// PlainTextHasher intentionally preserves the prototype requirement. Replace
// this binding with a bcrypt implementation before handling real credentials.
type PlainTextHasher struct{}

func (PlainTextHasher) Hash(password string) (string, error) {
	return password, nil
}

func (PlainTextHasher) Compare(hash, password string) error {
	if subtle.ConstantTimeCompare([]byte(hash), []byte(password)) != 1 {
		return errors.New("password mismatch")
	}

	return nil
}
