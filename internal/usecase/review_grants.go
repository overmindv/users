package usecase

import (
	"context"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

const reviewReasonAssignmentEnded = "assignment_ended"

type ReviewGrantStore interface {
	ChangeReviewGrant(context.Context, domain.ReviewGrantChange) error
	ListReviewGrants(ctx context.Context, topicID, after uuid.UUID, limit int) ([]uuid.UUID, error)
	CheckReviewGrants(ctx context.Context, topicID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error)
}

type ReviewTopicChecker interface {
	CheckActiveTopic(context.Context, uuid.UUID) error
}

type ReviewGrants struct {
	store  ReviewGrantStore
	topics ReviewTopicChecker
}

func NewReviewGrants(store ReviewGrantStore, topics ReviewTopicChecker) *ReviewGrants {
	return &ReviewGrants{store: store, topics: topics}
}

func (s *ReviewGrants) Change(ctx context.Context, change domain.ReviewGrantChange) error {
	if change.ActorID == uuid.Nil || change.SessionID == uuid.Nil || change.SessionVersion < 1 {
		return domain.ErrUnauthorized
	}
	if change.Grant && change.ActorID == change.UserID {
		return domain.ErrPermissionDenied
	}
	if change.UserID == uuid.Nil || change.TopicID == uuid.Nil || !validReviewReason(change) {
		return domain.ErrInvalidReviewGrant
	}
	if change.Grant {
		if err := s.topics.CheckActiveTopic(ctx, change.TopicID); err != nil {
			return err
		}
	}

	return s.store.ChangeReviewGrant(ctx, change)
}

func validReviewReason(change domain.ReviewGrantChange) bool {
	if change.Grant {
		return change.Reason == "qualification_verified" || change.Reason == "editorial_assignment"
	}

	return change.Reason == reviewReasonAssignmentEnded || change.Reason == "access_review"
}

func (s *ReviewGrants) List(ctx context.Context, topicID, after uuid.UUID, limit int) (domain.ReviewGrantPage, error) {
	if limit < 1 || limit > 100 || topicID == uuid.Nil {
		return domain.ReviewGrantPage{}, domain.ErrInvalidReviewGrant
	}
	ids, err := s.store.ListReviewGrants(ctx, topicID, after, limit+1)
	if err != nil {
		return domain.ReviewGrantPage{}, err
	}
	page := domain.ReviewGrantPage{UserIDs: ids}
	if len(ids) > limit {
		page.UserIDs = ids[:limit]
		page.NextAfter = ids[limit-1].String()
	}

	return page, nil
}

func (s *ReviewGrants) Check(ctx context.Context, topicID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if topicID == uuid.Nil || len(ids) == 0 || len(ids) > 64 {
		return nil, domain.ErrInvalidReviewGrant
	}
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, domain.ErrInvalidReviewGrant
		}
	}

	return s.store.CheckReviewGrants(ctx, topicID, ids)
}
