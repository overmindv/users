package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

type reviewGrantStub struct {
	ids    []uuid.UUID
	err    error
	change domain.ReviewGrantChange
}

func (s *reviewGrantStub) ChangeReviewGrant(_ context.Context, change domain.ReviewGrantChange) error {
	s.change = change

	return s.err
}

func (s *reviewGrantStub) ListReviewGrants(_ context.Context, _, _ uuid.UUID, limit int) ([]uuid.UUID, error) {
	return s.ids[:min(limit, len(s.ids))], s.err
}

func (s *reviewGrantStub) CheckReviewGrants(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
	return s.ids, s.err
}

type reviewTopicStub struct{ err error }

func (s *reviewTopicStub) CheckActiveTopic(context.Context, uuid.UUID) error { return s.err }

func TestReviewGrantChange(t *testing.T) {
	t.Parallel()

	store := &reviewGrantStub{}
	topic := &reviewTopicStub{err: domain.ErrInactiveReviewTopic}
	grants := NewReviewGrants(store, topic)
	valid := domain.ReviewGrantChange{
		ActorID: uuid.New(), SessionID: uuid.New(), SessionVersion: 1, UserID: uuid.New(), TopicID: uuid.New(),
		Reason: "qualification_verified", Grant: true,
	}
	noSession, self, noReason := valid, valid, valid
	noSession.SessionID, self.UserID, noReason.Reason = uuid.Nil, valid.ActorID, ""
	for _, tc := range []struct {
		change domain.ReviewGrantChange
		want   error
	}{
		{noSession, domain.ErrUnauthorized}, {self, domain.ErrPermissionDenied},
		{noReason, domain.ErrInvalidReviewGrant}, {valid, domain.ErrInactiveReviewTopic},
	} {
		if err := grants.Change(t.Context(), tc.change); !errors.Is(err, tc.want) || store.change.UserID != uuid.Nil {
			t.Errorf("invalid grant: %v, want %v", err, tc.want)
		}
	}
	revoke := valid
	revoke.Grant, revoke.Reason = false, reviewReasonAssignmentEnded
	if err := grants.Change(t.Context(), revoke); err != nil || store.change != revoke {
		t.Errorf("revoke when topic is inactive: %v", err)
	}
	topic.err = nil
	if err := grants.Change(t.Context(), valid); err != nil || store.change != valid {
		t.Errorf("grant: %v", err)
	}
}

func TestReviewGrantReads(t *testing.T) {
	t.Parallel()

	store := &reviewGrantStub{ids: []uuid.UUID{uuid.New(), uuid.New()}}
	grants := NewReviewGrants(store, &reviewTopicStub{})
	topic := uuid.New()
	for _, limit := range []int{0, 101} {
		if _, err := grants.List(t.Context(), topic, uuid.Nil, limit); !errors.Is(err, domain.ErrInvalidReviewGrant) {
			t.Errorf("list limit %d: %v", limit, err)
		}
	}
	page, err := grants.List(t.Context(), topic, uuid.Nil, 1)
	if err != nil || len(page.UserIDs) != 1 || page.NextAfter != store.ids[0].String() {
		t.Errorf("page: %+v, %v", page, err)
	}
	for _, ids := range [][]uuid.UUID{nil, {uuid.Nil}, make([]uuid.UUID, 65)} {
		if _, err := grants.Check(t.Context(), topic, ids); !errors.Is(err, domain.ErrInvalidReviewGrant) {
			t.Errorf("invalid batch size %d: %v", len(ids), err)
		}
	}
	if ids, err := grants.Check(t.Context(), topic, store.ids); err != nil || len(ids) != len(store.ids) {
		t.Errorf("check returned %v: %v", ids, err)
	}
	store.err = context.DeadlineExceeded
	if _, err := grants.List(t.Context(), topic, uuid.Nil, 1); !errors.Is(err, store.err) {
		t.Errorf("list with unavailable storage: %v", err)
	}
}
