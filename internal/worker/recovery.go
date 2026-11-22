package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type RecoveryQueue interface {
	Claim(context.Context) (postgresrepo.RecoveryMail, bool, error)
	Complete(context.Context, postgresrepo.RecoveryMail) error
	Retry(context.Context, postgresrepo.RecoveryMail) error
	Cleanup(context.Context) error
}
type RecoveryOpener interface {
	Open([]byte) (domain.RecoveryMessage, error)
}
type RecoverySender interface {
	Send(context.Context, domain.RecoveryMessage) error
}
type Recovery struct {
	queue  RecoveryQueue
	cipher RecoveryOpener
	sender RecoverySender
	log    *slog.Logger
}

func NewRecovery(queue RecoveryQueue, cipher RecoveryOpener, sender RecoverySender, log *slog.Logger) *Recovery {
	if log == nil {
		log = slog.Default()
	}

	return &Recovery{queue: queue, cipher: cipher, sender: sender, log: log}
}

func (w *Recovery) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Minute)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := w.runOne(runCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				w.log.Warn("recovery mail iteration failed")
			}
		case <-cleanup.C:
			cleanCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := w.queue.Cleanup(cleanCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				w.log.Warn("recovery mail cleanup failed")
			}
		}
	}
}

func (w *Recovery) runOne(ctx context.Context) error {
	mail, found, err := w.queue.Claim(ctx)
	if err != nil || !found {
		return err
	}
	message, err := w.cipher.Open(mail.Payload)
	if err == nil {
		err = w.sender.Send(ctx, message)
	}
	if err != nil {
		// Ошибка SMTP может содержать email или серверный payload: не логируем её.
		return w.queue.Retry(ctx, mail)
	}

	return w.queue.Complete(ctx, mail)
}
