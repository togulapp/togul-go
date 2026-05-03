package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func boolResponse(enabled bool) evaluateResponse {
	v := "false"
	if enabled {
		v = "true"
	}
	return evaluateResponse{
		FlagKey:   "test",
		Enabled:   enabled,
		ValueType: "boolean",
		Value:     json.RawMessage(v),
		Reason:    "rule_match",
	}
}

func TestClient_IsEnabled_CacheHit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("expected X-API-Key header, got %q", got)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Fatalf("did not expect Authorization header, got %q", auth)
		}
		callCount++
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "test-key",
		Environment: "test",
		CacheTTL:    5 * time.Second,
		baseURL:     server.URL,
	})

	ctx := context.Background()
	userCtx := map[string]string{"user_id": "u1"}

	// First call — cache miss
	val, err := client.IsEnabled(ctx, "test", userCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val {
		t.Error("expected true")
	}

	// Second call — cache hit
	val, err = client.IsEnabled(ctx, "test", userCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val {
		t.Error("expected true from cache")
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call (cache hit), got %d", callCount)
	}
}

func TestClient_IsEnabled_FailClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		Environment:  "test",
		FallbackMode: FailClosed,
		RetryCount:   1,
		baseURL:      server.URL,
	})

	val, err := client.IsEnabled(context.Background(), "test", nil)
	if err == nil {
		t.Error("expected error")
	}
	if val {
		t.Error("expected false (fail-closed)")
	}
}

func TestClient_IsEnabled_FailOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		Environment:  "test",
		FallbackMode: FailOpen,
		RetryCount:   1,
		baseURL:      server.URL,
	})

	val, err := client.IsEnabled(context.Background(), "test", nil)
	if err == nil {
		t.Error("expected error")
	}
	if !val {
		t.Error("expected true (fail-open)")
	}
}

func TestClient_InvalidateCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "test-key",
		Environment: "test",
		CacheTTL:    5 * time.Second,
		baseURL:     server.URL,
	})

	ctx := context.Background()
	client.IsEnabled(ctx, "test", map[string]string{"user_id": "u1"})
	client.InvalidateCache()
	client.IsEnabled(ctx, "test", map[string]string{"user_id": "u1"})

	if callCount != 2 {
		t.Errorf("expected 2 API calls after cache invalidation, got %d", callCount)
	}
}

func TestClient_IsEnabled_CacheKeyIncludesFullContext(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "test-key",
		Environment: "test",
		CacheTTL:    5 * time.Second,
		baseURL:     server.URL,
	})

	ctx := context.Background()
	_, _ = client.IsEnabled(ctx, "test", map[string]string{"user_id": "u1", "country": "TR"})
	_, _ = client.IsEnabled(ctx, "test", map[string]string{"user_id": "u1", "country": "US"})

	if callCount != 2 {
		t.Fatalf("expected 2 API calls for different contexts, got %d", callCount)
	}
}

func TestClient_IsEnabled_DoesNotRetryClientErrors(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"code":    "evaluate.environment_forbidden",
			"message": "API key does not have access to this environment",
		})
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "test-key",
		Environment: "test",
		CacheTTL:    5 * time.Second,
		baseURL:     server.URL,
	})

	_, err := client.IsEnabled(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Fatalf("expected a single request for client error, got %d", callCount)
	}
}

func TestClient_EvaluateResult_BoolValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "my-flag",
			Enabled:   true,
			ValueType: "boolean",
			Value:     json.RawMessage("true"),
			Reason:    "rule_match",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	result, err := client.Evaluate(context.Background(), "my-flag", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.BoolValue(false); got != true {
		t.Errorf("expected true, got %v", got)
	}
}

func TestClient_EvaluateResult_StringValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "ui-theme",
			Enabled:   true,
			ValueType: "string",
			Value:     json.RawMessage(`"dark_mode"`),
			Reason:    "rule_match",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	val, err := client.EvaluateString(context.Background(), "ui-theme", nil, "light")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "dark_mode" {
		t.Errorf("expected dark_mode, got %q", val)
	}
}

func TestClient_EvaluateResult_NumberValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "max-retries",
			Enabled:   true,
			ValueType: "number",
			Value:     json.RawMessage("5"),
			Reason:    "rule_match",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	val, err := client.EvaluateNumber(context.Background(), "max-retries", nil, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 5 {
		t.Errorf("expected 5, got %v", val)
	}
}

func TestClient_EvaluateResult_JSONValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "user-config",
			Enabled:   true,
			ValueType: "json",
			Value:     json.RawMessage(`{"plan":"pro","limit":100}`),
			Reason:    "rule_match",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})

	var cfg struct {
		Plan  string `json:"plan"`
		Limit int    `json:"limit"`
	}
	if err := client.EvaluateJSON(context.Background(), "user-config", nil, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Plan != "pro" || cfg.Limit != 100 {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestClient_EvaluateResult_TypeMismatch_ReturnsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "flag",
			Enabled:   true,
			ValueType: "number",
			Value:     json.RawMessage("42"),
			Reason:    "rule_match",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	// Requesting string value from a number flag — should get fallback
	val, err := client.EvaluateString(context.Background(), "flag", nil, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "default" {
		t.Errorf("expected fallback 'default', got %q", val)
	}
}

func TestClient_EvaluateResult_DisabledFlag_ReturnsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "flag",
			Enabled:   false,
			ValueType: "string",
			Value:     json.RawMessage(`"some_value"`),
			Reason:    "flag_disabled",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	val, err := client.EvaluateString(context.Background(), "flag", nil, "fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "fallback" {
		t.Errorf("expected 'fallback', got %q", val)
	}
}

func TestClient_Evaluate_CacheMissOnEmptyValueType(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "k",
		Environment: "test",
		CacheTTL:    5 * time.Second,
		baseURL:     server.URL,
	})

	// Manually inject a stale entry with empty ValueType into the cache.
	cacheKey := cacheKeyFor("flag", "test", nil)
	client.cache.Store(cacheKey, cacheEntry{
		result:    EvaluateResult{FlagKey: "flag", Enabled: true, ValueType: ""},
		expiresAt: time.Now().Add(5 * time.Second),
	})

	// Should treat the stale entry as a cache miss and call the API.
	_, err := client.Evaluate(context.Background(), "flag", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call (stale entry bypassed), got %d", callCount)
	}
}
