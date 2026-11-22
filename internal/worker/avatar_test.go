package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/config"
	"github.com/overmindv/users/internal/media"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type avatarQueueFake struct {
	event              postgresrepo.AvatarEvent
	completed, retried bool
}

func (f *avatarQueueFake) Claim(context.Context) (postgresrepo.AvatarEvent, bool, error) {
	return f.event, true, nil
}
func (f *avatarQueueFake) Complete(_ context.Context, e postgresrepo.AvatarEvent) error {
	f.completed = e == f.event

	return nil
}
func (f *avatarQueueFake) Retry(_ context.Context, e postgresrepo.AvatarEvent, _ error) error {
	f.retried = e == f.event

	return nil
}
func (f *avatarQueueFake) Reconcile(context.Context, string) (string, error) { return "", nil }
func (f *avatarQueueFake) Exhausted(context.Context) (int64, error)          { return 0, nil }

func TestAvatarWorkerVersionedDelivery(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusNoContent, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			file := "file-current"
			queue := &avatarQueueFake{event: postgresrepo.AvatarEvent{ID: "event", UserID: "owner", FileID: &file, Version: 17, Attempts: 8}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/v1/internal/users/owner/avatar-binding/versioned" || r.Header.Get("X-Media-Service-Token") != "fixture-token" {
					t.Error("wrong internal request")
				}
				var body struct {
					FileID  string `json:"file_id"`
					Version int64  `json:"version"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FileID != file || body.Version != 17 {
					t.Error("wrong snapshot", err)
				}
				w.WriteHeader(status)
				if status != 204 {
					_, _ = w.Write([]byte("sensitive-upstream-body"))
				}
			}))
			defer server.Close()
			client := media.New(config.Media{URL: server.URL, Token: "fixture-token", Timeout: time.Second})
			worker := NewAvatar(queue, client, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
			err := worker.runOne(t.Context())
			if status == 204 {
				if err != nil || !queue.completed || queue.retried {
					t.Fatal("success", err)
				}
			} else {
				if err == nil || !queue.retried || queue.completed || strings.Contains(err.Error(), "sensitive") {
					t.Fatal("outage", err)
				}
			}
		})
	}
}
