# sink — Outbound event forwarding to external consumers

All files stable — prefer this summary over reading source files.

## sink.go

```go
type EventSink interface {
    Send(ctx context.Context, event hookevt.HookEvent) error
    Close() error
}
```

## http.go

```go
type HTTPSink struct { /* unexported fields */ }
func NewHTTPSink(endpoint string) *HTTPSink
func (s *HTTPSink) Send(ctx context.Context, event hookevt.HookEvent) error
func (s *HTTPSink) Close() error
```

HTTPSink POSTs JSON to an endpoint (e.g. hooks-store /ingest). 3s timeout, connection pooling (keep-alive), drains response body for connection reuse. Thread-safe.

## http_test.go

Tests: Send happy path, server down, timeout, Close.

Imports: `hookevt` (HookEvent).
