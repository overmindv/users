package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/overmindv/users/internal/config"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New создаёт внутренний Media client сервиса Users.
func New(cfg config.Media) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: cfg.Timeout},
	}
}

// ValidateAvatar проверяет ownership, purpose, visibility и ready status файла.
func (c *Client) ValidateAvatar(ctx context.Context, userID, fileID string) error {
	result := map[string]bool{}
	path := "/v1/internal/users/" + url.PathEscape(userID) + "/avatar-files/" + url.PathEscape(fileID) + "/validate"
	if err := c.request(ctx, http.MethodGet, path, "", nil, &result); err != nil {
		return fmt.Errorf("validate media avatar: %w", err)
	}

	return nil
}

// ReplaceAvatarBinding идемпотентно синхронизирует binding Media с Users source of truth.
func (c *Client) ReplaceAvatarBinding(ctx context.Context, userID string, fileID *string) error {
	return c.request(ctx, http.MethodPut, "/v1/internal/users/"+url.PathEscape(userID)+"/avatar-binding", "", map[string]any{"file_id": fileID}, nil)
}

// Ready проверяет доступность Media API.
func (c *Client) Ready(ctx context.Context) error {
	return c.request(ctx, http.MethodGet, "/ready", "", nil, nil)
}

func (c *Client) request(ctx context.Context, method, path, userID string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("marshal media request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create media request: %w", err)
	}
	request.Header.Set("X-Media-Service-Token", c.token)
	if userID != "" {
		request.Header.Set("X-User-ID", userID)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call media: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))

		return fmt.Errorf("media returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
			return fmt.Errorf("decode media response: %w", err)
		}
	}

	return nil
}
