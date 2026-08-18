package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	usersmedia "github.com/overmindv/users/internal/media"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type Avatar struct {
	outbox *postgresrepo.AvatarOutbox
	media  *usersmedia.Client
	poll   time.Duration
	log    *slog.Logger
}

// NewAvatar создаёт доставщик avatar binding событий.
func NewAvatar(outbox *postgresrepo.AvatarOutbox, media *usersmedia.Client, poll time.Duration, log *slog.Logger) *Avatar {
	return &Avatar{
		outbox: outbox,
		media:  media,
		poll:   poll,
		log:    log,
	}
}

// Run доставляет события до отмены context.
func (w *Avatar) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		if err := w.runOne(ctx); err != nil {
			w.log.Error("avatar outbox iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// runOne забирает одно событие и идемпотентно синхронизирует binding в media.
func (w *Avatar) runOne(ctx context.Context) error {
	event, found, err := w.outbox.Claim(ctx)
	if err != nil {
		return fmt.Errorf("claim avatar event: %w", err)
	}
	if !found {
		return nil
	}
	if err := w.media.ReplaceAvatarBinding(ctx, event.UserID, event.FileID); err != nil {
		if retryErr := w.outbox.Retry(ctx, event, err); retryErr != nil {
			return errors.Join(
				fmt.Errorf("replace avatar binding: %w", err),
				fmt.Errorf("retry avatar event: %w", retryErr),
			)
		}

		return fmt.Errorf("replace avatar binding: %w", err)
	}
	if err := w.outbox.Complete(ctx, event.ID); err != nil {
		return fmt.Errorf("complete avatar event: %w", err)
	}

	return nil
}
