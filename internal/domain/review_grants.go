package domain

import (
	"errors"

	"github.com/google/uuid"
)

var (
	ErrInvalidReviewGrant      = errors.New("invalid review grant")
	ErrInactiveReviewTopic     = errors.New("review topic does not exist or is inactive")
	ErrReviewTopicsUnavailable = errors.New("review topic catalog unavailable")
)

type ReviewGrantChange struct {
	ActorID        uuid.UUID
	SessionID      uuid.UUID
	SessionVersion int64
	UserID         uuid.UUID
	TopicID        uuid.UUID
	Reason         string
	Grant          bool
}

type ReviewGrantPage struct {
	UserIDs   []uuid.UUID `json:"user_ids"`
	NextAfter string      `json:"next_after,omitempty"`
}
