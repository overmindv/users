package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var avatarExhausted = promauto.NewGauge(prometheus.GaugeOpts{Name: "users_avatar_outbox_exhausted", Help: "Retained exhausted avatar events, including superseded events."})

type avatarOutbox interface {
	Claim(context.Context) (postgresrepo.AvatarEvent, bool, error)
	Complete(context.Context, postgresrepo.AvatarEvent) error
	Retry(context.Context, postgresrepo.AvatarEvent, error) error
	Reconcile(context.Context, string) (string, error)
	Exhausted(context.Context) (int64, error)
}
type avatarMedia interface {
	ReplaceAvatarBinding(context.Context, string, *string, int64) error
}

type Avatar struct {
	outbox avatarOutbox
	media  avatarMedia
	poll   time.Duration
	log    *slog.Logger
}

// NewAvatar создаёт доставщик avatar binding событий.
func NewAvatar(outbox avatarOutbox, media avatarMedia, poll time.Duration, log *slog.Logger) *Avatar {
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
	var cursor string
	var nextReconcile time.Time
	for {
		if !time.Now().Before(nextReconcile) {
			iteration, cancel := context.WithTimeout(ctx, 10*time.Second)
			next, err := w.outbox.Reconcile(iteration, cursor)
			if err != nil {
				w.log.Error("avatar reconciliation failed", "error", err)
			} else {
				cursor = next
			}
			if count, err := w.outbox.Exhausted(iteration); err == nil {
				avatarExhausted.Set(float64(count))
			}
			cancel()
			nextReconcile = time.Now().Add(time.Minute)
		}
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	event, found, err := w.outbox.Claim(ctx)
	if err != nil {
		return fmt.Errorf("claim avatar event: %w", err)
	}
	if !found {
		return nil
	}
	if err := w.media.ReplaceAvatarBinding(ctx, event.UserID, event.FileID, event.Version); err != nil {
		if retryErr := w.outbox.Retry(ctx, event, err); retryErr != nil {
			return errors.Join(
				errors.New("replace avatar binding failed"),
				fmt.Errorf("retry avatar event: %w", retryErr),
			)
		}

		return errors.New("replace avatar binding failed")
	}
	if err := w.outbox.Complete(ctx, event); err != nil {
		return fmt.Errorf("complete avatar event: %w", err)
	}

	return nil
}
