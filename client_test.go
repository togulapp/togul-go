package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
		json.NewEncoder(w).Encode(evaluateResponse{
			FlagKey: "test",
			Enabled: true,
			Value:   true,
			Reason:  "rule_match",
		})
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
		json.NewEncoder(w).Encode(evaluateResponse{Value: true})
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
		json.NewEncoder(w).Encode(evaluateResponse{Value: true})
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
