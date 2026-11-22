package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type privacyQueueFake struct{ published, retried, saved bool }

const privacyTestUser = "owner"

func (f *privacyQueueFake) ClaimPrivacyExport(context.Context) (postgresrepo.ExportJob, bool, error) {
	return postgresrepo.ExportJob{RequestID: "request", UserID: privacyTestUser, Owner: "tasks", Cursor: "cursor", Watermark: "snapshot"}, true, nil
}
func (f *privacyQueueFake) SavePrivacyExport(context.Context, postgresrepo.ExportJob, domain.ExportPage) error {
	f.saved = true

	return nil
}
func (f *privacyQueueFake) RetryPrivacyExport(context.Context, postgresrepo.ExportJob) error {
	f.retried = true

	return nil
}
func (f *privacyQueueFake) ExpirePrivacyExports(context.Context) error { return nil }
func (f *privacyQueueFake) ClaimDeletion(context.Context) (postgresrepo.DeletionJob, bool, error) {
	return postgresrepo.DeletionJob{ID: "event", UserID: privacyTestUser, Payload: []byte(`{"event_id":"event"}`)}, true, nil
}
func (f *privacyQueueFake) FinishDeletion(_ context.Context, _ postgresrepo.DeletionJob, success bool) error {
	f.published = success

	return nil
}

type privacyPublisherFake struct {
	fail       bool
	topic, key string
	payload    []byte
}

func (f *privacyPublisherFake) Publish(_ context.Context, topic, key string, payload []byte) error {
	f.topic, f.key, f.payload = topic, key, payload
	if f.fail {
		return errors.New("private broker details")
	}

	return nil
}

type privacyExporterFake struct{ fail bool }

func (f privacyExporterFake) Export(_ context.Context, owner, user, cursor, watermark string) (domain.ExportPage, error) {
	if owner != "tasks" || user != privacyTestUser || cursor != "cursor" || watermark != "snapshot" {
		return domain.ExportPage{}, errors.New("wrong export request")
	}
	if f.fail {
		return domain.ExportPage{}, errors.New("private owner response")
	}

	return domain.ExportPage{UserID: user, Watermark: watermark, Records: []domain.ExportRecord{}}, nil
}

func TestPrivacyWorkerPartialFailure(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "outage"}[fail], func(t *testing.T) {
			t.Parallel()
			queue := &privacyQueueFake{}
			publisher := &privacyPublisherFake{fail: fail}
			worker := NewPrivacy(queue, privacyExporterFake{fail: fail}, publisher, nil)
			err := worker.runDeletion(t.Context())
			if (err != nil) != fail || queue.published == fail || publisher.topic != "user.deleted.v1" || publisher.key != privacyTestUser || string(publisher.payload) != `{"event_id":"event"}` {
				t.Fatal("publication acknowledgement", err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("broker details leaked")
			}
			err = worker.runExport(t.Context())
			if (err != nil) != fail || queue.saved == fail || queue.retried != fail {
				t.Fatal("export retry", err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("owner details leaked")
			}
		})
	}
}
