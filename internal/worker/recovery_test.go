package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type recoveryQueueFake struct{ retries, completions int }

const recoveryClaim = "claim"

func (*recoveryQueueFake) Claim(context.Context) (postgresrepo.RecoveryMail, bool, error) {
	return postgresrepo.RecoveryMail{ID: "mail", ClaimToken: recoveryClaim, Payload: []byte("encrypted")}, true, nil
}
func (q *recoveryQueueFake) Complete(_ context.Context, mail postgresrepo.RecoveryMail) error {
	if mail.ClaimToken != recoveryClaim {
		return errors.New("lost fence")
	}
	q.completions++

	return nil
}
func (q *recoveryQueueFake) Retry(_ context.Context, mail postgresrepo.RecoveryMail) error {
	if mail.ClaimToken != recoveryClaim {
		return errors.New("lost fence")
	}
	q.retries++

	return nil
}
func (*recoveryQueueFake) Cleanup(context.Context) error { return nil }

type recoveryOpenerFake struct{}

func (recoveryOpenerFake) Open([]byte) (domain.RecoveryMessage, error) {
	return domain.RecoveryMessage{To: "a@example.com", Token: "token", Purpose: domain.RecoveryReset}, nil
}

type recoverySenderFake struct{ err error }

func (s recoverySenderFake) Send(context.Context, domain.RecoveryMessage) error { return s.err }

func TestRecoverySMTPOutage(t *testing.T) {
	t.Parallel()
	queue := &recoveryQueueFake{}
	worker := NewRecovery(queue, recoveryOpenerFake{}, recoverySenderFake{err: errors.New("smtp unavailable")}, nil)
	if err := worker.runOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if queue.retries != 1 || queue.completions != 0 {
		t.Fatal("outage lost mail")
	}
	worker = NewRecovery(queue, recoveryOpenerFake{}, recoverySenderFake{}, nil)
	if err := worker.runOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if queue.completions != 1 {
		t.Fatal("recovery did not deliver")
	}
}
