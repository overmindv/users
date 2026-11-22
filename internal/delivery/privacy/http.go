package privacy

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/privacyowner"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
	"github.com/overmindv/users/internal/usecase"
)

type Store interface {
	AcknowledgePrivacy(context.Context, string, string, string, string) error
	DeletionLedger(context.Context, int64, int64, int) (postgresrepo.DeletionLedgerPage, error)
	DownloadPrivacyExport(context.Context, string, string) (postgresrepo.PrivacyExport, error)
}
type Sessions interface {
	Validate(context.Context, string) (*auth.Claims, error)
}
type Throttle interface {
	Check(context.Context, string, string) (bool, error)
}

type handler struct {
	service  *usecase.Privacy
	store    Store
	sessions Sessions
	owners   map[string][32]byte
	throttle Throttle
}

func Register(mux interface{ Handle(string, http.Handler) }, service *usecase.Privacy, store Store, sessions Sessions, owners map[string]privacyowner.Endpoint, throttle Throttle, contextSecret string) {
	h := handler{service: service, store: store, sessions: sessions, owners: map[string][32]byte{}, throttle: throttle}
	for name, endpoint := range owners {
		h.owners[name] = sha256.Sum256([]byte(endpoint.Token))
	}
	routes := map[string]http.HandlerFunc{
		"POST /internal/privacy/requests":              h.prepare,
		"POST /internal/privacy/requests/{id}/confirm": h.confirm,
		"GET /internal/privacy/requests/{id}":          h.status,
		"GET /internal/privacy/requests/{id}/export":   h.download,
		"POST /internal/privacy/deletions/{id}/ack":    h.ack,
		"GET /internal/privacy/deletions":              h.ledger,
	}
	for path, next := range routes {
		mux.Handle(path, auth.LoginClientIP(contextSecret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			next(w, r.WithContext(ctx))
		})))
	}
}

func (h handler) actor(r *http.Request) (domain.PrivacyActor, error) {
	token, err := auth.BearerToken(r.Header.Get("Authorization"))
	if err != nil {
		return domain.PrivacyActor{}, err
	}
	claims, err := h.sessions.Validate(r.Context(), token)
	if err != nil {
		return domain.PrivacyActor{}, err
	}

	return domain.PrivacyActor{UserID: claims.Subject, SessionID: claims.ID, Version: claims.SessionVersion}, nil
}

func (h handler) prepare(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		respond(w, nil, err)

		return
	}
	var body struct {
		Kind     string `json:"kind"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	allowed, err := h.throttle.Check(r.Context(), actor.UserID, auth.ClientIP(r.Context()))
	if err != nil {
		respond(w, nil, err)

		return
	}
	if !allowed {
		respond(w, nil, domain.ErrTooManyRequests)

		return
	}
	result, err := h.service.Prepare(r.Context(), actor, body.Kind, body.Password)
	respond(w, result, err)
}
func (h handler) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !body.Confirm {
		respond(w, nil, domain.ErrPrivacyRequest)

		return
	}
	result, err := h.service.Confirm(r.Context(), r.PathValue("id"), r.Header.Get("X-Privacy-Receipt"))
	respond(w, result, err)
}
func (h handler) status(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Status(r.Context(), r.PathValue("id"), r.Header.Get("X-Privacy-Receipt"))
	respond(w, result, err)
}
func (h handler) download(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		respond(w, nil, err)

		return
	}
	if uuid.Validate(r.PathValue("id")) != nil {
		respond(w, nil, domain.ErrPrivacyRequest)

		return
	}
	result, err := h.store.DownloadPrivacyExport(r.Context(), actor.UserID, r.PathValue("id"))
	if err == nil {
		w.Header().Set("Content-Disposition", `attachment; filename="account-export.json"`)
	}
	respond(w, result, err)
}
func (h handler) owner(r *http.Request) (string, error) {
	token := r.Header.Get("X-Privacy-Service-Token")
	if token == "" {
		return "", domain.ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	for owner, expected := range h.owners {
		if subtle.ConstantTimeCompare(digest[:], expected[:]) == 1 {
			return owner, nil
		}
	}

	return "", domain.ErrUnauthorized
}
func (h handler) ack(w http.ResponseWriter, r *http.Request) {
	owner, err := h.owner(r)
	if err != nil {
		respond(w, nil, err)

		return
	}
	var body struct {
		State     string `json:"state"`
		ErrorCode string `json:"error_code"`
	}
	if !decode(w, r, &body) {
		return
	}
	err = h.store.AcknowledgePrivacy(r.Context(), r.PathValue("id"), owner, body.State, body.ErrorCode)
	respond(w, map[string]bool{"accepted": err == nil}, err)
}
func (h handler) ledger(w http.ResponseWriter, r *http.Request) {
	if _, err := h.owner(r); err != nil {
		respond(w, nil, err)

		return
	}
	read := func(key string, def int64) (int64, error) {
		if r.URL.Query().Get(key) == "" {
			return def, nil
		}

		return strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	}
	after, e1 := read("after", 0)
	watermark, e2 := read("watermark", 0)
	limit, e3 := read("limit", 100)
	if e1 != nil || e2 != nil || e3 != nil || limit < 1 || limit > 100 {
		respond(w, nil, domain.ErrPrivacyRequest)

		return
	}
	result, err := h.store.DeletionLedger(r.Context(), after, watermark, int(limit))
	respond(w, result, err)
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		respond(w, nil, domain.ErrPrivacyRequest)

		return false
	}

	return true
}
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status, code := http.StatusServiceUnavailable, "privacy_unavailable"
		switch {
		case errors.Is(err, domain.ErrUnauthorized):
			status, code = http.StatusUnauthorized, "unauthorized"
		case errors.Is(err, domain.ErrPermissionDenied):
			status, code = http.StatusForbidden, "permission_denied"
		case errors.Is(err, domain.ErrTooManyRequests):
			status, code = http.StatusTooManyRequests, "rate_limited"
		case errors.Is(err, domain.ErrPrivacyRequest):
			status, code = http.StatusConflict, "invalid_privacy_request"
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})

		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
