package security

import (
	"bytes"
	"testing"

	"github.com/overmindv/users/internal/domain"
)

func TestRecoveryMailCipher(t *testing.T) {
	t.Parallel()
	cipher, err := NewRecoveryMailCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	message := domain.RecoveryMessage{To: "learner@example.com", Purpose: domain.RecoveryReset, Token: "secret-token"}
	sealed, err := cipher.Seal(message)
	if err != nil || bytes.Contains(sealed, []byte(message.Token)) || bytes.Contains(sealed, []byte(message.To)) {
		t.Fatal("unencrypted payload", err)
	}
	opened, err := cipher.Open(sealed)
	if err != nil || opened != message {
		t.Fatal("roundtrip", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = cipher.Open(sealed); err == nil {
		t.Fatal("tampering accepted")
	}
	if _, err = cipher.Open(nil); err == nil {
		t.Fatal("empty payload accepted")
	}
	if _, err = NewRecoveryMailCipher([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}
