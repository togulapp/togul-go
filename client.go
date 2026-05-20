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

const defaultBaseURL = "https://api.togul.io"

type Config struct {
	APIKey      string
	Environment string
	Timeout     time.Duration
	CacheTTL    time.Duration
	RetryCount  int
	baseURL     string
}

func (c *Config) getBaseURL() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return defaultBaseURL
}

type Client struct {
	cfg       Config
	http      *http.Client
	cache     sync.Map
	listeners []CacheListener
	mu        sync.RWMutex
}

type CacheListener func(flagKey string)

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

// EvaluateResult mirrors the API evaluate response.
type EvaluateResult struct {
	FlagKey   string
	Enabled   bool
	ValueType string
	Value     any
	Reason    string
}

type cacheEntry struct {
	result    EvaluateResult
	expiresAt time.Time
}

type evaluateRequest struct {
	FlagKey        string            `json:"flag_key"`
	EnvironmentKey string            `json:"environment_key"`
	Context        map[string]string `json:"context"`
}

type evaluateResponse struct {
	FlagKey   string          `json:"flag_key"`
	Enabled   bool            `json:"enabled"`
	ValueType string          `json:"value_type"`
	Value     json.RawMessage `json:"value"`
	Reason    string          `json:"reason"`
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


// Evaluate returns the full EvaluateResult for a flag, including typed value accessors.
func (c *Client) Evaluate(ctx context.Context, key string, userCtx map[string]string) (EvaluateResult, error) {
	cacheKey := cacheKeyFor(key, c.cfg.Environment, userCtx)

	if entry, ok := c.cache.Load(cacheKey); ok {
		ce := entry.(cacheEntry)
		// Empty ValueType means stale/invalid entry — treat as cache miss.
		if time.Now().Before(ce.expiresAt) && ce.result.ValueType != "" {
			return ce.result, nil
		}
		c.cache.Delete(cacheKey)
	}

	result, err := c.evaluate(ctx, key, userCtx)
	if err != nil {
		return EvaluateResult{}, err
	}

	c.cache.Store(cacheKey, cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(c.cfg.CacheTTL),
	})

	return result, nil
}

func (c *Client) evaluate(ctx context.Context, key string, userCtx map[string]string) (EvaluateResult, error) {
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return EvaluateResult{}, errors.New("togul-sdk: APIKey is required")
	}

	body, err := json.Marshal(evaluateRequest{
		FlagKey:        key,
		EnvironmentKey: c.cfg.Environment,
		Context:        userCtx,
	})
	if err != nil {
		return EvaluateResult{}, fmt.Errorf("togul-sdk: marshal error: %w", err)
	}

	var lastErr error
	for attempt := range c.cfg.RetryCount {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*100) * time.Millisecond)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.getBaseURL()+"/api/v1/evaluate", bytes.NewReader(body))
		if err != nil {
			return EvaluateResult{}, fmt.Errorf("togul-sdk: request error: %w", err)
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
				return EvaluateResult{}, apiErr
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

		var value any
		if len(evalResp.Value) > 0 {
			_ = json.Unmarshal(evalResp.Value, &value)
		}
		return EvaluateResult{
			FlagKey:   evalResp.FlagKey,
			Enabled:   evalResp.Enabled,
			ValueType: evalResp.ValueType,
			Value:     value,
			Reason:    evalResp.Reason,
		}, nil
	}

	return EvaluateResult{}, fmt.Errorf("togul-sdk: all retries failed: %w", lastErr)
}

// InvalidateCache clears all cached flag values.
func (c *Client) InvalidateCache() {
	c.cache.Range(func(key, _ any) bool {
		c.cache.Delete(key)
		return true
	})
	c.notifyListeners("")
}

// InvalidateFlag clears a specific flag from cache.
func (c *Client) InvalidateFlag(key string) {
	prefix := key + ":"
	c.cache.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), prefix) {
			c.cache.Delete(k)
		}
		return true
	})
	c.notifyListeners(key)
}

// OnCacheInvalidated registers a listener for cache invalidation events.
func (c *Client) OnCacheInvalidated(listener CacheListener) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listeners = append(c.listeners, listener)
}

func (c *Client) notifyListeners(flagKey string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, l := range c.listeners {
		l(flagKey)
	}
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
