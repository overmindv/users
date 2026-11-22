package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type privacyQueue interface {
	ClaimPrivacyExport(context.Context) (postgresrepo.ExportJob, bool, error)
	SavePrivacyExport(context.Context, postgresrepo.ExportJob, domain.ExportPage) error
	RetryPrivacyExport(context.Context, postgresrepo.ExportJob) error
	ExpirePrivacyExports(context.Context) error
	ClaimDeletion(context.Context) (postgresrepo.DeletionJob, bool, error)
	FinishDeletion(context.Context, postgresrepo.DeletionJob, bool) error
}
type privacyExporter interface {
	Export(context.Context, string, string, string, string) (domain.ExportPage, error)
}
type privacyPublisher interface {
	Publish(context.Context, string, string, []byte) error
}

type Privacy struct {
	queue     privacyQueue
	owners    privacyExporter
	publisher privacyPublisher
	log       *slog.Logger
}

func NewPrivacy(queue privacyQueue, owners privacyExporter, publisher privacyPublisher, log *slog.Logger) *Privacy {
	return &Privacy{queue: queue, owners: owners, publisher: publisher, log: log}
}

func (w *Privacy) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var cleanup time.Time
	for {
		iteration, cancel := context.WithTimeout(ctx, 20*time.Second)
		if time.Now().After(cleanup) {
			if err := w.queue.ExpirePrivacyExports(iteration); err != nil {
				w.log.Error("privacy export expiry failed")
			}
			cleanup = time.Now().Add(time.Minute)
		}
		if err := w.runDeletion(iteration); err != nil {
			w.log.Error("privacy deletion publication failed")
		}
		if err := w.runExport(iteration); err != nil {
			w.log.Error("privacy export iteration failed")
		}
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Privacy) runDeletion(ctx context.Context) error {
	job, found, err := w.queue.ClaimDeletion(ctx)
	if err != nil || !found {
		return err
	}
	publishErr := w.publisher.Publish(ctx, "user.deleted.v1", job.UserID, job.Payload)
	err = w.queue.FinishDeletion(ctx, job, publishErr == nil)
	if publishErr != nil {
		return errors.New("deletion publication failed")
	}

	return err
}

func (w *Privacy) runExport(ctx context.Context) error {
	job, found, err := w.queue.ClaimPrivacyExport(ctx)
	if err != nil || !found {
		return err
	}
	page, err := w.owners.Export(ctx, job.Owner, job.UserID, job.Cursor, job.Watermark)
	if err == nil {
		err = w.queue.SavePrivacyExport(ctx, job, page)
	}
	if err != nil {
		_ = w.queue.RetryPrivacyExport(ctx, job)

		return errors.New("owner export failed")
	}

	return nil
}
