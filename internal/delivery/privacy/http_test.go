package privacy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/privacyowner"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

type storeFake struct{ owner string }

func (f *storeFake) AcknowledgePrivacy(_ context.Context, _ string, owner, _, _ string) error {
	f.owner = owner

	return nil
}
func (f *storeFake) DeletionLedger(context.Context, int64, int64, int) (postgresrepo.DeletionLedgerPage, error) {
	return postgresrepo.DeletionLedgerPage{}, nil
}
func (f *storeFake) DownloadPrivacyExport(context.Context, string, string) (postgresrepo.PrivacyExport, error) {
	return postgresrepo.PrivacyExport{}, nil
}

func TestOwnerAcknowledgementScope(t *testing.T) {
	t.Parallel()
	store := &storeFake{}
	mux := http.NewServeMux()
	Register(mux, nil, store, nil, map[string]privacyowner.Endpoint{"tasks": {Token: "tasks-secret"}, "content": {Token: "content-secret"}}, nil, "context-fixture")
	path := "/internal/privacy/deletions/" + uuid.NewString() + "/ack"
	for _, test := range []struct {
		token, body string
		want        int
	}{{"wrong", `{"state":"completed"}`, 401}, {"tasks-secret", `{"owner":"content","state":"completed"}`, 409}, {"tasks-secret", `{"state":"completed"}`, 200}} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(test.body))
		req.Header.Set("X-Privacy-Service-Token", test.token)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != test.want || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("response %d %s", res.Code, res.Body.String())
		}
	}
	if store.owner != "tasks" {
		t.Fatalf("owner impersonation: %s", store.owner)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/privacy/requests", strings.NewReader(`{"kind":"delete","password":"password"}`))
	req.Header.Set("X-Privacy-Service-Token", "tasks-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatal("service token substituted for owner session")
	}
}
