package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type streamEvent struct {
	Type          string `json:"type"`
	FlagKey       string `json:"flag_key,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
}

// Stream connects to the SSE endpoint and invalidates the cache when flags change.
// It automatically reconnects with exponential backoff on connection failures.
func (c *Client) Stream(ctx context.Context) error {
	backoff := time.Second

	for {
		err := c.streamOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
			return err
		}

		if err != nil {
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		backoff = time.Second
	}
}

func (c *Client) streamOnce(ctx context.Context) error {
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return errors.New("nori-sdk: APIKey is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.BaseURL, "/")+"/api/v1/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-API-Key", c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := decodeAPIError(resp)
		return fmt.Errorf("nori-sdk: stream failed: %w", err)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var event streamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		c.InvalidateCache()
	}

	return scanner.Err()
}
