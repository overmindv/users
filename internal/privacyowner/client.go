package privacyowner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/users/internal/domain"
)

const MaxPageBytes = 2 << 20

type Endpoint struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}
type Client struct {
	endpoints map[string]Endpoint
	http      *http.Client
}

func New(endpoints map[string]Endpoint) *Client {
	return &Client{endpoints: endpoints, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) Export(ctx context.Context, owner, userID, cursor, watermark string) (domain.ExportPage, error) {
	endpoint, ok := c.endpoints[owner]
	if !ok {
		return domain.ExportPage{}, errors.New("export owner unavailable")
	}
	query := url.Values{"user_id": {userID}, "cursor": {cursor}, "watermark": {watermark}, "limit": {"100"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint.URL, "/")+"/internal/privacy/export?"+query.Encode(), nil)
	if err != nil {
		return domain.ExportPage{}, err
	}
	request.Header.Set("X-Privacy-Service-Token", endpoint.Token)
	response, err := c.http.Do(request)
	if err != nil {
		return domain.ExportPage{}, errors.New("export owner request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return domain.ExportPage{}, errors.New("export owner response failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxPageBytes+1))
	if err != nil || len(body) > MaxPageBytes {
		return domain.ExportPage{}, errors.New("export page limit exceeded")
	}
	var page domain.ExportPage
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&page) != nil || decoder.Decode(new(any)) != io.EOF {
		return page, errors.New("invalid export page")
	}
	if err = ValidatePage(owner, userID, cursor, watermark, page); err != nil {
		return domain.ExportPage{}, err
	}

	return page, nil
}

func ValidatePage(owner, userID, cursor, watermark string, page domain.ExportPage) error {
	invalid := errors.New("invalid export page")
	kinds := map[string][]string{"tasks": {"submission", "attempt", "task_revision", "task_review"}, "recommendations": {"study_activity", "plan", "topic_profile"}, "content": {"draft", "contribution"}, "media": {"file"}, "rating": {"score"}, "feed": {"entry"}, "sandbox": {"execution"}}
	if page.UserID != userID || page.Watermark == "" || len(page.Watermark) > 256 || len(page.NextCursor) > 1024 || (page.NextCursor != "" && page.NextCursor == cursor) || (watermark != "" && page.Watermark != watermark) || len(page.Records) > 100 || page.Records == nil {
		return invalid
	}
	if _, ok := kinds[owner]; !ok {
		return invalid
	}
	textLimit := 64 << 10
	if owner == "tasks" {
		textLimit = 256 << 10
	}
	for _, record := range page.Records {
		if record.UserID != userID || uuid.Validate(record.ID) != nil || !slices.Contains(kinds[owner], record.Kind) || record.OccurredAt.IsZero() || len(record.Title) > 1000 || len(record.Text) > textLimit || len(record.Result) > 2000 || (record.ResourceID != "" && uuid.Validate(record.ResourceID) != nil) {
			return invalid
		}
	}

	return nil
}
