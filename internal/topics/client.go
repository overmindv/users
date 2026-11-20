package topics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

type Client struct {
	base *url.URL
	http *http.Client
}

func New(address string) (*Client, error) {
	base, err := url.Parse(address)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" ||
		base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errors.New("USERS_ENTITIES_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}

	return &Client{base: base, http: &http.Client{
		Timeout:       1500 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) CheckActiveTopic(ctx context.Context, topicID uuid.UUID) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.JoinPath("v1", "topics", topicID.String()).String(), nil)
	if err != nil {
		return domain.ErrReviewTopicsUnavailable
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return domain.ErrReviewTopicsUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return domain.ErrInactiveReviewTopic
	}
	if resp.StatusCode != http.StatusOK {
		return domain.ErrReviewTopicsUnavailable
	}
	const limit = 32 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(data) > limit {
		return domain.ErrReviewTopicsUnavailable
	}
	var topic struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	if err := json.Unmarshal(data, &topic); err != nil || topic.ID != topicID || topic.Status == "" {
		return domain.ErrReviewTopicsUnavailable
	}
	if topic.Status != "active" {
		return domain.ErrInactiveReviewTopic
	}

	return nil
}
