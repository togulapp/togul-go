# Togul Go SDK

Go client for evaluating Togul feature flags and consuming the SSE invalidation stream.

## Install

```bash
go get github.com/togulapp/togul-go
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/togulapp/togul-go"
)

func main() {
	client := sdk.NewClient(sdk.Config{
		APIKey:       "your-environment-api-key",
		Environment:  "production",
		CacheTTL:     30 * time.Second,
		FallbackMode: sdk.FailClosed,
		RetryCount:   2,
	})

	result, err := client.Evaluate(context.Background(), "new-dashboard", map[string]string{
		"user_id": "user-123",
		"country": "TR",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Enabled)   // true
	fmt.Println(result.ValueType) // "string"
	fmt.Println(result.Value)     // "dark_mode"
	fmt.Println(result.Reason)    // "rule_match"
}
```

## EvaluateResult

`Evaluate` returns an `EvaluateResult` struct:

```go
type EvaluateResult struct {
	FlagKey   string
	Enabled   bool
	ValueType string // "boolean" | "string" | "number" | "json"
	Value     any
	Reason    string
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
