package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const argon2idPrefix = "$argon2id$"

// Argon2IDHasher хэширует пароли Argon2id — memory-hard алгоритм из Password Hashing Competition.
// Argon2id устойчив к перебору на GPU/FPGA (нужна память), что делает его лучшим выбором
// для хранения паролей. Формат значения: $argon2id$v=19$m=..,t=..,p=..$<salt>$<hash>.
// Соль и параметры хранятся в самой строке, поэтому Compare повторно вычисляет hash при входе.
//
// Также умеет читать legacy-значения (открытый текст) для одноразовой миграции старых записей.
type Argon2IDHasher struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
}

// NewArgon2IDHasher возвращает hasher с параметрами, рекомендованными для производства (OWASP):
// 64 MiB памяти, 2 итерации, 2 потока, ключ 32 байта, соль 16 байт.
func NewArgon2IDHasher() Argon2IDHasher {
	return Argon2IDHasher{time: 2, memory: 64 * 1024, threads: 2, keyLen: 32}
}

// Hash вычисляет Argon2id hash пароля с новой случайной солью.
func (h Argon2IDHasher) Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, h.keyLen)

	encoded := argon2idPrefix + "v=19" +
		"$m=" + strconv.FormatUint(uint64(h.memory), 10) +
		",t=" + strconv.FormatUint(uint64(h.time), 10) +
		",p=" + strconv.FormatUint(uint64(h.threads), 10) +
		"$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(key)

	return encoded, nil
}

// Compare проверяет пароль против сохранённого значения.
// Значения в legacy-формате (открытый текст) сравниваются constant-time, чтобы не утечь по таймингу,
// и существуют только для одноразовой миграции существующих записей.
func (h Argon2IDHasher) Compare(stored, password string) error {
	if !strings.HasPrefix(stored, argon2idPrefix) {
		if subtle.ConstantTimeCompare([]byte(stored), []byte(password)) != 1 {
			return errors.New("password mismatch")
		}

		return nil
	}

	params, salt, key, err := decodeArgon2ID(stored)
	if err != nil {
		// Некорректные сохранённые данные не должны раскрывать причину отказа.
		return errors.New("password mismatch")
	}

	candidate := argon2.IDKey([]byte(password), salt, params.time, params.memory, uint8(params.threads), uint32(len(key)))
	if subtle.ConstantTimeCompare(candidate, key) != 1 {
		return errors.New("password mismatch")
	}

	return nil
}

// RequiresUpgrade сообщает, что сохранённое значение устарело (не Argon2id) и требует пере-хэширования.
func (Argon2IDHasher) RequiresUpgrade(stored string) bool {
	return !strings.HasPrefix(stored, argon2idPrefix)
}

type argon2Params struct{ memory, time, threads uint32 }

// decodeArgon2ID разбирает сохранённое значение на параметры, salt и ключ.
func decodeArgon2ID(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// Ожидаемая структура: ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash].
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2][:2] != "v=" {
		return argon2Params{}, nil, nil, errors.New("malformed argon2id string")
	}

	var params argon2Params
	for _, kv := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(kv, "=", 2)
		if len(pair) != 2 {
			return argon2Params{}, nil, nil, errors.New("malformed argon2id params")
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return argon2Params{}, nil, nil, errors.New("malformed argon2id param value")
		}
		switch pair[0] {
		case "m":
			params.memory = uint32(value)
		case "t":
			params.time = uint32(value)
		case "p":
			params.threads = uint32(value)
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, errors.New("malformed argon2id salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, errors.New("malformed argon2id key")
	}

	return params, salt, key, nil
}

// PlainTextHasher хранит пароль как есть и предназначен ТОЛЬКО для unit-тестов.
// В продакшене подключён Argon2IDHasher — см. internal/app/container.go.
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

// RequiresUpgrade всегда сообщает о необходимости миграции, т.к. открытый текст небезопасен.
func (PlainTextHasher) RequiresUpgrade(string) bool { return true }
