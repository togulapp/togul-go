# Nori Go SDK

Go client for evaluating Nori feature flags and consuming the SSE invalidation stream.

## Install

```bash
go get github.com/noriapp/nori-go
```

## Usage

```go
package main

import (
	"context"
	"time"

	sdk "github.com/noriapp/nori-go"
)

func main() {
	client := sdk.NewClient(sdk.Config{
		BaseURL:      "http://localhost:8080",
		APIKey:       "your-environment-api-key",
		Environment:  "production",
		CacheTTL:     30 * time.Second,
		FallbackMode: sdk.FailClosed,
		RetryCount:   2,
	})

	enabled, err := client.IsEnabled(context.Background(), "new-dashboard", map[string]string{
		"user_id": "user-123",
		"country": "TR",
	})
	_, _ = enabled, err
}
```

## Streaming

```go
go client.Stream(ctx)
```

`Stream` connects to `GET /api/v1/stream` with `X-API-Key` and invalidates the local cache when change events arrive.

## Notes

- `APIKey` must be an environment API key, not a user JWT.
- The cache key includes the full evaluation context.
- The client retries `429` and `5xx` responses, but does not retry `401`/`403`/`404`.
