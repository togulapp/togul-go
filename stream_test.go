package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Stream_UsesAPIKeyHeader(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "stream-key" {
			t.Fatalf("expected X-API-Key header, got %q", got)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Fatalf("did not expect Authorization header, got %q", auth)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"flag.updated\",\"flag_key\":\"x\"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(done)
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:  "stream-key",
		Timeout: time.Second,
		baseURL: server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.streamOnce(ctx)
	}()

	select {
	case <-done:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream request")
	}

	select {
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream to exit")
	}
}
