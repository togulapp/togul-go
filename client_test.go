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

func TestClient_Evaluate_CacheHit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("expected X-API-Key header, got %q", got)
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

	result, err := client.Evaluate(ctx, "test", userCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Enabled {
		t.Error("expected Enabled=true")
	}

	result, err = client.Evaluate(ctx, "test", userCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Enabled {
		t.Error("expected Enabled=true from cache")
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call (cache hit), got %d", callCount)
	}
}

func TestClient_Evaluate_PropagatesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "test-key",
		Environment: "test",
		RetryCount:  1,
		baseURL:     server.URL,
	})

	_, err := client.Evaluate(context.Background(), "test", nil)
	if err == nil {
		t.Error("expected error on server failure")
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
	client.Evaluate(ctx, "test", map[string]string{"user_id": "u1"})
	client.InvalidateCache()
	client.Evaluate(ctx, "test", map[string]string{"user_id": "u1"})

	if callCount != 2 {
		t.Errorf("expected 2 API calls after cache invalidation, got %d", callCount)
	}
}

func TestClient_Evaluate_CacheKeyIncludesFullContext(t *testing.T) {
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
	_, _ = client.Evaluate(ctx, "test", map[string]string{"user_id": "u1", "country": "TR"})
	_, _ = client.Evaluate(ctx, "test", map[string]string{"user_id": "u1", "country": "US"})

	if callCount != 2 {
		t.Fatalf("expected 2 API calls for different contexts, got %d", callCount)
	}
}

func TestClient_Evaluate_DoesNotRetryClientErrors(t *testing.T) {
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

	_, err := client.Evaluate(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Fatalf("expected a single request for client error, got %d", callCount)
	}
}

func TestClient_Evaluate_BoolValue(t *testing.T) {
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
	if result.Value.(bool) != true {
		t.Errorf("expected true, got %v", result.Value)
	}
}

func TestClient_Evaluate_StringValue(t *testing.T) {
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
	result, err := client.Evaluate(context.Background(), "ui-theme", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value.(string) != "dark_mode" {
		t.Errorf("expected dark_mode, got %v", result.Value)
	}
}

func TestClient_Evaluate_NumberValue(t *testing.T) {
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
	result, err := client.Evaluate(context.Background(), "max-retries", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value.(float64) != 5 {
		t.Errorf("expected 5, got %v", result.Value)
	}
}

func TestClient_Evaluate_JSONValue(t *testing.T) {
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
	result, err := client.Evaluate(context.Background(), "user-config", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.Value.(map[string]any)
	if m["plan"] != "pro" || m["limit"] != float64(100) {
		t.Errorf("unexpected value: %v", m)
	}
}

func TestClient_Evaluate_DisabledFlagStillReturnsValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey:   "flag",
			Enabled:   false,
			ValueType: "string",
			Value:     json.RawMessage(`"onur"`),
			Reason:    "disabled",
		})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	result, err := client.Evaluate(context.Background(), "flag", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Enabled {
		t.Error("expected Enabled=false")
	}
	if result.Value.(string) != "onur" {
		t.Errorf("expected value to be present even when disabled, got %v", result.Value)
	}
}

func TestClient_Evaluate_SendsCorrectRequestBody(t *testing.T) {
	var received evaluateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "prod", baseURL: server.URL})
	client.Evaluate(context.Background(), "my-flag", map[string]string{"user_id": "u1", "plan": "pro"})

	if received.FlagKey != "my-flag" {
		t.Errorf("expected flag_key=my-flag, got %q", received.FlagKey)
	}
	if received.EnvironmentKey != "prod" {
		t.Errorf("expected environment_key=prod, got %q", received.EnvironmentKey)
	}
	if received.Context["user_id"] != "u1" || received.Context["plan"] != "pro" {
		t.Errorf("unexpected context: %v", received.Context)
	}
}

func TestClient_Evaluate_DoesNotSendAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("did not expect Authorization header, got %q", auth)
		}
		if key := r.Header.Get("X-API-Key"); key != "k" {
			t.Errorf("expected X-API-Key=k, got %q", key)
		}
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", Environment: "test", baseURL: server.URL})
	client.Evaluate(context.Background(), "flag", nil)
}

func TestClient_InvalidateFlag_OnlyRemovesTargetFlag(t *testing.T) {
	callCount := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req evaluateRequest
		json.NewDecoder(r.Body).Decode(&req)
		callCount[req.FlagKey]++
		json.NewEncoder(w).Encode(boolResponse(true))
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "k",
		Environment: "test",
		CacheTTL:    5 * time.Second,
		baseURL:     server.URL,
	})

	ctx := context.Background()
	client.Evaluate(ctx, "flag-a", nil)
	client.Evaluate(ctx, "flag-b", nil)
	client.InvalidateFlag("flag-a")
	client.Evaluate(ctx, "flag-a", nil) // miss — refetch
	client.Evaluate(ctx, "flag-b", nil) // hit  — cached

	if callCount["flag-a"] != 2 {
		t.Errorf("expected 2 calls for flag-a, got %d", callCount["flag-a"])
	}
	if callCount["flag-b"] != 1 {
		t.Errorf("expected 1 call for flag-b, got %d", callCount["flag-b"])
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

	cacheKey := cacheKeyFor("flag", "test", nil)
	client.cache.Store(cacheKey, cacheEntry{
		result:    EvaluateResult{FlagKey: "flag", Enabled: true, ValueType: ""},
		expiresAt: time.Now().Add(5 * time.Second),
	})

	_, err := client.Evaluate(context.Background(), "flag", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call (stale entry bypassed), got %d", callCount)
	}
}
