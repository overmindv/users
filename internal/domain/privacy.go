package domain

import (
	"errors"
	"time"
)

var ErrPrivacyRequest = errors.New("invalid or expired privacy request")

var PrivacyOwners = []string{"tasks", "recommendations", "content", "media", "rating", "feed", "sandbox"}

type PrivacyRequest struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	State       string               `json:"state"`
	Publication string               `json:"publication,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	ExpiresAt   time.Time            `json:"expires_at"`
	Owners      []PrivacyOwnerStatus `json:"owners"`
}

type PrivacyOwnerStatus struct {
	Owner     string `json:"owner"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

type PrivacyStart struct {
	Request PrivacyRequest `json:"request"`
	Receipt string         `json:"receipt"`
}

type PrivacyPreparation struct {
	ID, UserID, Kind, PasswordHash string
	SessionID                      string
	SecurityVersion                int64
	ReceiptHash                    []byte
}

type PrivacyActor struct {
	UserID, SessionID string
	Version           int64
}

// ExportRecord намеренно не допускает произвольный JSON и credential-поля.
type ExportRecord struct {
	UserID     string    `json:"user_id"`
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
	ResourceID string    `json:"resource_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	Text       string    `json:"text,omitempty"`
	Result     string    `json:"result,omitempty"`
}

type ExportPage struct {
	UserID     string         `json:"user_id"`
	Watermark  string         `json:"watermark"`
	NextCursor string         `json:"next_cursor"`
	Records    []ExportRecord `json:"records"`
}

type DeletionEvent struct {
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	OccurredAt       time.Time       `json:"occurred_at"`
	Payload          DeletionPayload `json:"payload"`
}

type DeletionPayload struct {
	UserID    string `json:"user_id"`
	RequestID string `json:"request_id"`
	Tombstone bool   `json:"tombstone"`
}
