//go:build component

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/delivery/reviewgrants"
	"github.com/overmindv/users/internal/domain"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/topics"
	"github.com/overmindv/users/internal/usecase"
)

func TestTopicReviewGrantsHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	pool := throttlePool(t)
	repo := postgresrepo.NewUserRepository(pool)
	sessions := auth.NewManager("jwt-test-fixture", "users-test", time.Hour)
	sessions.SetSessionStore(repo)
	create := func(admin bool) (*domain.User, string) {
		t.Helper()
		id := uuid.NewString()
		input := domain.NewUserParams{
			ID: id, Email: domain.Email(id + "@example.com"), Username: domain.Username("u" + id[:8] + id[9:13]),
			PasswordHash: "$argon2id$test", Now: time.Now(),
		}
		user, err := domain.NewUser(input)
		if err != nil {
			t.Fatal(err)
		}
		if err = user.SetAdmin(admin, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := repo.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
		token, _, err := sessions.IssueUser(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		return user, token
	}
	admin, adminJWT := create(true)
	first, firstJWT := create(false)
	second, _ := create(false)
	topic, other := uuid.NewString(), uuid.NewString()
	var catalogStatus atomic.Int32
	catalogStatus.Store(http.StatusOK)
	var revokeDuringLookup atomic.Bool
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/topics/"+topic {
			http.NotFound(w, r)
			return
		}
		if revokeDuringLookup.Load() {
			if err := sessions.Revoke(r.Context(), adminJWT); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		if status := int(catalogStatus.Load()); status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": topic, "status": "active"})
	}))
	defer catalog.Close()
	client, err := topics.New(catalog.URL)
	if err != nil {
		t.Fatal(err)
	}
	const readToken = "review-reader-test-fixture"
	mux := http.NewServeMux()
	reviewgrants.Register(mux, usecase.NewReviewGrants(repo, client), sessions, readToken)
	request := func(method, path, token, body string, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		got := httptest.NewRecorder()
		mux.ServeHTTP(got, req)
		if got.Code != status || got.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: got %d %s, want %d", method, path, got.Code, got.Body.String(), status)
		}
		return got.Body.Bytes()
	}
	const grant = `{"reason":"qualification_verified"}`
	const revoke = `{"reason":"access_review"}`
	base := "/internal/review-grants/" + topic
	firstPath, secondPath := base+"/"+first.ID(), base+"/"+second.ID()
	request(http.MethodGet, base, "", "", http.StatusUnauthorized)
	request(http.MethodGet, base, adminJWT, "", http.StatusUnauthorized)
	request(http.MethodPut, firstPath, readToken, grant, http.StatusUnauthorized)
	request(http.MethodPut, firstPath, firstJWT, grant, http.StatusForbidden)
	request(http.MethodPut, base+"/"+admin.ID(), adminJWT, grant, http.StatusForbidden)
	request(http.MethodPut, base+"/"+uuid.NewString(), adminJWT, grant, http.StatusNotFound)
	request(http.MethodPut, "/internal/review-grants/"+other+"/"+first.ID(), adminJWT, grant, http.StatusNotFound)
	request(http.MethodPut, firstPath, adminJWT, `{"reason":"arbitrary text"}`, http.StatusBadRequest)
	request(http.MethodPut, firstPath, adminJWT, `{"reason":"editorial_assignment","actor_id":"ignored"}`, http.StatusBadRequest)
	request(http.MethodPost, base+"/check", readToken, strings.Repeat(" ", 4097), http.StatusBadRequest)
	catalogStatus.Store(http.StatusServiceUnavailable)
	request(http.MethodPut, firstPath, adminJWT, grant, http.StatusServiceUnavailable)
	catalogStatus.Store(http.StatusOK)

	request(http.MethodPut, firstPath, adminJWT, grant, http.StatusNoContent)
	request(http.MethodPut, firstPath, adminJWT, grant, http.StatusNoContent)
	request(http.MethodPut, secondPath, adminJWT, grant, http.StatusNoContent)
	var page1, page2 domain.ReviewGrantPage
	if err := json.Unmarshal(request(http.MethodGet, base+"?limit=1", readToken, "", http.StatusOK), &page1); err != nil || len(page1.UserIDs) != 1 || page1.NextAfter != page1.UserIDs[0].String() {
		t.Fatalf("first page: %+v, %v", page1, err)
	}
	if err := json.Unmarshal(request(http.MethodGet, base+"?limit=1&after="+page1.NextAfter, readToken, "", http.StatusOK), &page2); err != nil || len(page2.UserIDs) != 1 || page2.UserIDs[0] == page1.UserIDs[0] || page2.NextAfter != "" {
		t.Fatalf("second page: %+v, %v", page2, err)
	}
	request(http.MethodGet, base+"?limit=101", readToken, "", http.StatusBadRequest)
	body := fmt.Sprintf(`{"user_ids":[%q,%q]}`, first.ID(), second.ID())
	read := func(path string, want ...string) {
		t.Helper()
		var page domain.ReviewGrantPage
		data := request(http.MethodPost, path+"/check", readToken, body, http.StatusOK)
		if err := json.Unmarshal(data, &page); err != nil || len(page.UserIDs) != len(want) || page.UserIDs == nil {
			t.Fatalf("check got %s, want %v: %v", data, want, err)
		}
		for i, id := range want {
			if page.UserIDs[i].String() != id {
				t.Errorf("check got %s, want %v", data, want)
			}
		}
	}
	read("/internal/review-grants/" + other)
	if _, err := pool.Exec(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, second.ID()); err != nil {
		t.Fatal(err)
	}
	read(base, first.ID())
	var list domain.ReviewGrantPage
	if err := json.Unmarshal(request(http.MethodGet, base, readToken, "", http.StatusOK), &list); err != nil || len(list.UserIDs) != 1 || list.UserIDs[0].String() != first.ID() {
		t.Errorf("deleted account in page: %+v, %v", list, err)
	}
	request(http.MethodPut, secondPath, adminJWT, grant, http.StatusNotFound)
	catalogStatus.Store(http.StatusServiceUnavailable)
	request(http.MethodDelete, firstPath, adminJWT, revoke, http.StatusNoContent)
	request(http.MethodDelete, firstPath, adminJWT, revoke, http.StatusNoContent)
	read(base)
	var audits, grants, revocations int
	err = pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE action='topic_review_granted' AND details->>'reason'='qualification_verified'),
		       count(*) FILTER (WHERE action='topic_review_revoked' AND details->>'reason'='access_review')
		FROM user_security_audit WHERE actor_id=$1 AND subject_id=$2 AND details->>'topic_id'=$3
	`, admin.ID(), first.ID(), topic).Scan(&audits, &grants, &revocations)
	if err != nil || audits != 2 || grants != 1 || revocations != 1 {
		t.Errorf("audit rows=%d grants=%d revocations=%d: %v", audits, grants, revocations, err)
	}
	catalogStatus.Store(http.StatusOK)
	revokeDuringLookup.Store(true)
	request(http.MethodPut, firstPath, adminJWT, grant, http.StatusForbidden)
	read(base)
}
