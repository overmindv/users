package reviewgrants

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/auth"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/usecase"
)

func Register(mux interface{ Handle(string, http.Handler) }, grants *usecase.ReviewGrants, sessions *auth.Manager, readToken string) {
	h := handler{grants: grants, sessions: sessions, readDigest: sha256.Sum256([]byte(readToken)), readEnabled: readToken != ""}
	mux.Handle("PUT /internal/review-grants/{topic_id}/{user_id}", http.HandlerFunc(h.change))
	mux.Handle("DELETE /internal/review-grants/{topic_id}/{user_id}", http.HandlerFunc(h.change))
	mux.Handle("GET /internal/review-grants/{topic_id}", h.internal(h.list))
	mux.Handle("POST /internal/review-grants/{topic_id}/check", h.internal(h.check))
}

type handler struct {
	grants      *usecase.ReviewGrants
	sessions    *auth.Manager
	readDigest  [32]byte
	readEnabled bool
}

func (h handler) internal(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token, err := requestToken(r)
		presented := sha256.Sum256([]byte(token))
		if !h.readEnabled || err != nil || subtle.ConstantTimeCompare(h.readDigest[:], presented[:]) != 1 {
			writeError(w, domain.ErrUnauthorized)

			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	})
}

func (h handler) change(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	token, err := requestToken(r)
	if err != nil {
		writeError(w, err)

		return
	}
	claims, err := h.sessions.Validate(ctx, token)
	if err != nil {
		writeError(w, err)

		return
	}
	if !slices.Contains(claims.Roles, domain.RoleAdmin) && !slices.Contains(claims.Roles, "superuser") {
		writeError(w, domain.ErrPermissionDenied)

		return
	}
	actor, actorErr := uuid.Parse(claims.Subject)
	session, sessionErr := uuid.Parse(claims.ID)
	if actorErr != nil || sessionErr != nil {
		writeError(w, domain.ErrUnauthorized)

		return
	}
	topic, topicErr := uuid.Parse(r.PathValue("topic_id"))
	user, userErr := uuid.Parse(r.PathValue("user_id"))
	if topicErr != nil || userErr != nil {
		writeError(w, domain.ErrInvalidReviewGrant)

		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &body) {
		return
	}
	err = h.grants.Change(ctx, domain.ReviewGrantChange{
		ActorID: actor, SessionID: session, SessionVersion: claims.SessionVersion,
		UserID: user, TopicID: topic, Reason: body.Reason, Grant: r.Method == http.MethodPut,
	})
	if err != nil {
		writeError(w, err)

		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	topic, err := uuid.Parse(r.PathValue("topic_id"))
	if err != nil {
		writeError(w, domain.ErrInvalidReviewGrant)

		return
	}
	limit, after := 50, uuid.Nil
	query := r.URL.Query()
	if query.Has("limit") {
		limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil {
			writeError(w, domain.ErrInvalidReviewGrant)

			return
		}
	}
	if query.Has("after") {
		after, err = uuid.Parse(query.Get("after"))
		if err != nil {
			writeError(w, domain.ErrInvalidReviewGrant)

			return
		}
	}
	page, err := h.grants.List(r.Context(), topic, after, limit)
	if err != nil {
		writeError(w, err)

		return
	}
	writeJSON(w, page)
}

func (h handler) check(w http.ResponseWriter, r *http.Request) {
	topic, err := uuid.Parse(r.PathValue("topic_id"))
	if err != nil {
		writeError(w, domain.ErrInvalidReviewGrant)

		return
	}
	var body struct {
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	ids, err := h.grants.Check(r.Context(), topic, body.UserIDs)
	if err != nil {
		writeError(w, err)

		return
	}
	writeJSON(w, domain.ReviewGrantPage{UserIDs: ids})
}

func requestToken(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 4096 {
		return "", domain.ErrUnauthorized
	}

	return auth.BearerToken(values[0])
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, domain.ErrInvalidReviewGrant)

		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, domain.ErrPermissionDenied):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrInvalidReviewGrant):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrUserNotFound), errors.Is(err, domain.ErrInactiveReviewTopic):
		status = http.StatusNotFound
	}
	http.Error(w, http.StatusText(status), status)
}
