package privacyowner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

func TestExportRejectsForeignAndUnknownFields(t *testing.T) {
	t.Parallel()
	owner := uuid.NewString()
	for _, bad := range []string{`"password_hash":"secret",`, `"hidden_tests":[],`} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{%s"user_id":%q,"watermark":"v1","next_cursor":"","records":[]}`, bad, owner)
			}))
			defer server.Close()
			client := New(map[string]Endpoint{"tasks": {URL: server.URL, Token: "fixture"}})
			if _, err := client.Export(context.Background(), "tasks", owner, "", ""); err == nil {
				t.Fatal("unknown field accepted")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"user_id":%q,"watermark":"v1","next_cursor":"","records":[]}`, uuid.NewString())
	}))
	defer server.Close()
	if _, err := New(map[string]Endpoint{"tasks": {URL: server.URL}}).Export(context.Background(), "tasks", owner, "", ""); err == nil {
		t.Fatal("foreign subject accepted")
	}
}

func TestExportOwnerContract(t *testing.T) {
	t.Parallel()
	subject := uuid.NewString()
	page := domain.ExportPage{UserID: subject, Watermark: "snapshot-1", Records: []domain.ExportRecord{{UserID: subject, ID: uuid.NewString(), Kind: "submission", OccurredAt: time.Now().UTC(), Text: "my solution"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/privacy/export" || r.URL.Query().Get("user_id") != subject || r.URL.Query().Get("limit") != "100" || r.Header.Get("X-Privacy-Service-Token") != "fixture-token" {
			t.Error("incorrect owner request")
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	client := New(map[string]Endpoint{"tasks": {URL: server.URL, Token: "fixture-token"}})
	result, err := client.Export(t.Context(), "tasks", subject, "", "")
	if err != nil || len(result.Records) != 1 || result.Records[0].Text != "my solution" {
		t.Fatal("valid export rejected", err)
	}
	if _, err = client.Export(t.Context(), "tasks", subject, "", "different-snapshot"); err == nil {
		t.Fatal("snapshot changed")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, http.StatusFound) }))
	defer redirect.Close()
	if _, err = New(map[string]Endpoint{"tasks": {URL: redirect.URL}}).Export(t.Context(), "tasks", subject, "", ""); err == nil {
		t.Fatal("redirect accepted")
	}
}
