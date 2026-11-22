package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/overmindv/users/internal/domain"
)

type RecoveryStore interface {
	QueueRecovery(context.Context, string, string, []byte, []byte) error
	ConsumeRecovery(context.Context, []byte, string, string) error
}

type RecoveryCipher interface {
	Seal(domain.RecoveryMessage) ([]byte, error)
}

type RecoveryThrottle interface {
	Check(context.Context, string, string) (bool, error)
}

type Recovery struct {
	store     RecoveryStore
	cipher    RecoveryCipher
	passwords PasswordHasher
	requests  RecoveryThrottle
	consumes  RecoveryThrottle
}

func NewRecovery(store RecoveryStore, cipher RecoveryCipher, passwords PasswordHasher, requests, consumes RecoveryThrottle) *Recovery {
	return &Recovery{store: store, cipher: cipher, passwords: passwords, requests: requests, consumes: consumes}
}

// Request не возвращает токен и не раскрывает существование email или срабатывание лимита.
func (s *Recovery) Request(ctx context.Context, address, purpose, ip string) error {
	if s == nil {
		return errors.New("recovery unavailable")
	}
	if purpose != domain.RecoveryReset && purpose != domain.RecoveryVerify {
		return domain.ErrInvalidRecoveryToken
	}
	email, err := domain.NewEmail(address)
	if err != nil {
		return err
	}
	allowed, err := s.requests.Check(ctx, email.String(), ip)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	sealed, err := s.cipher.Seal(domain.RecoveryMessage{To: email.String(), Purpose: purpose, Token: token})
	if err != nil {
		return err
	}

	return s.store.QueueRecovery(ctx, email.String(), purpose, hash[:], sealed)
}

func (s *Recovery) Reset(ctx context.Context, token, password, ip string) error {
	if len(password) < 8 || len(password) > 1024 {
		return domain.ErrInvalidPassword
	}
	hash, err := s.authorizeConsume(ctx, token, ip)
	if err != nil {
		return err
	}
	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return err
	}

	return s.store.ConsumeRecovery(ctx, hash, domain.RecoveryReset, passwordHash)
}

func (s *Recovery) Verify(ctx context.Context, token, ip string) error {
	hash, err := s.authorizeConsume(ctx, token, ip)
	if err != nil {
		return err
	}

	return s.store.ConsumeRecovery(ctx, hash, domain.RecoveryVerify, "")
}

func (s *Recovery) authorizeConsume(ctx context.Context, token, ip string) ([]byte, error) {
	if s == nil {
		return nil, errors.New("recovery unavailable")
	}
	if len(token) != 43 {
		return nil, domain.ErrInvalidRecoveryToken
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(token) != 43 || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return nil, domain.ErrInvalidRecoveryToken
	}
	allowed, err := s.consumes.Check(ctx, token, ip)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, domain.ErrInvalidRecoveryToken
	}
	hash := sha256.Sum256([]byte(token))

	return hash[:], nil
}
