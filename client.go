package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type FallbackMode int

const (
	FailClosed FallbackMode = iota // return false on error
	FailOpen                       // return true on error
)

type Config struct {
	BaseURL      string
	APIKey       string
	Environment  string
	Timeout      time.Duration
	CacheTTL     time.Duration
	FallbackMode FallbackMode
	RetryCount   int
}

type Client struct {
	cfg   Config
	http  *http.Client
	cache sync.Map
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("togul-sdk: api error %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("togul-sdk: api error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("togul-sdk: api error %d", e.StatusCode)
}

type cacheEntry struct {
	value     bool
	expiresAt time.Time
}

type evaluateRequest struct {
	FlagKey        string            `json:"flag_key"`
	EnvironmentKey string            `json:"environment_key"`
	Context        map[string]string `json:"context"`
}

type evaluateResponse struct {
	FlagKey string `json:"flag_key"`
	Enabled bool   `json:"enabled"`
	Value   bool   `json:"value"`
	Reason  string `json:"reason"`
}

func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 30 * time.Second
	}
	if cfg.RetryCount == 0 {
		cfg.RetryCount = 2
	}

	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// IsEnabled evaluates a feature flag for the given context.
func (c *Client) IsEnabled(ctx context.Context, key string, userCtx map[string]string) (bool, error) {
	cacheKey := cacheKeyFor(key, c.cfg.Environment, userCtx)

	if entry, ok := c.cache.Load(cacheKey); ok {
		ce := entry.(cacheEntry)
		if time.Now().Before(ce.expiresAt) {
			return ce.value, nil
		}
		c.cache.Delete(cacheKey)
	}

	value, err := c.evaluate(ctx, key, userCtx)
	if err != nil {
		switch c.cfg.FallbackMode {
		case FailOpen:
			return true, err
		default:
			return false, err
		}
	}

	c.cache.Store(cacheKey, cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(c.cfg.CacheTTL),
	})

	return value, nil
}

func (c *Client) evaluate(ctx context.Context, key string, userCtx map[string]string) (bool, error) {
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return false, errors.New("togul-sdk: APIKey is required")
	}

	body, err := json.Marshal(evaluateRequest{
		FlagKey:        key,
		EnvironmentKey: c.cfg.Environment,
		Context:        userCtx,
	})
	if err != nil {
		return false, fmt.Errorf("togul-sdk: marshal error: %w", err)
	}

	var lastErr error
	for attempt := range c.cfg.RetryCount {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*100) * time.Millisecond)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/api/v1/evaluate", bytes.NewReader(body))
		if err != nil {
			return false, fmt.Errorf("togul-sdk: request error: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", c.cfg.APIKey)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			apiErr := decodeAPIError(resp)
			resp.Body.Close()
			lastErr = apiErr
			if !shouldRetry(resp.StatusCode) {
				return false, apiErr
			}
			continue
		}

		var evalResp evaluateResponse
		if err := json.NewDecoder(resp.Body).Decode(&evalResp); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("togul-sdk: decode error: %w", err)
			continue
		}
		resp.Body.Close()

		return evalResp.Value, nil
	}

	return false, fmt.Errorf("togul-sdk: all retries failed: %w", lastErr)
}

// InvalidateCache clears all cached flag values.
func (c *Client) InvalidateCache() {
	c.cache.Range(func(key, _ any) bool {
		c.cache.Delete(key)
		return true
	})
}

func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func decodeAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Code:       payload.Code,
		Message:    payload.Message,
	}
}

func cacheKeyFor(flagKey, environment string, userCtx map[string]string) string {
	parts := []string{flagKey, environment}
	if len(userCtx) == 0 {
		return strings.Join(parts, ":")
	}

	keys := make([]string, 0, len(userCtx))
	for key := range userCtx {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		parts = append(parts, key+"="+userCtx[key])
	}

	return strings.Join(parts, ":")
}
