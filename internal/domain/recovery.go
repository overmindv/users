package domain

import "errors"

const (
	RecoveryReset  = "reset"
	RecoveryVerify = "verify"
)

var ErrInvalidRecoveryToken = errors.New("invalid recovery token")

// RecoveryMessage существует открытым только в памяти отправителя/получателя.
// В БД outbox сериализуется только зашифрованный payload, в логи не передаётся.
type RecoveryMessage struct {
	To      string `json:"to"`
	Purpose string `json:"purpose"`
	Token   string `json:"token"`
}
