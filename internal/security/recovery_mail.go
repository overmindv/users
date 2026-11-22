package security

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"

	"github.com/overmindv/users/internal/domain"
)

var recoveryMailAAD = []byte("users-recovery-mail-v1")

type RecoveryMailCipher struct{ aead cipher.AEAD }

func NewRecoveryMailCipher(key []byte) (*RecoveryMailCipher, error) {
	if len(key) != 32 {
		return nil, errors.New("recovery mail requires a 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}

	return &RecoveryMailCipher{aead: aead}, nil
}

func (c *RecoveryMailCipher) Seal(message domain.RecoveryMessage) ([]byte, error) {
	raw, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

	return c.aead.Seal(nil, nil, raw, recoveryMailAAD), nil
}

func (c *RecoveryMailCipher) Open(sealed []byte) (domain.RecoveryMessage, error) {
	raw, err := c.aead.Open(nil, nil, sealed, recoveryMailAAD)
	if err != nil {
		return domain.RecoveryMessage{}, errors.New("invalid recovery mail payload")
	}
	var message domain.RecoveryMessage
	if err = json.Unmarshal(raw, &message); err != nil {
		return domain.RecoveryMessage{}, errors.New("invalid recovery mail payload")
	}

	return message, nil
}
