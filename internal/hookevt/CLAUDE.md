# hookevt — Shared hook event type for the monitor

All files stable — prefer this summary over reading source files.

## hookevt.go

```go
type HookEvent struct {
    HookType  string                 `json:"hook_type"`
    Timestamp time.Time              `json:"timestamp"`
    Data      map[string]interface{} `json:"data"`
}
```

Used by monitor, server, sink, and tui packages. No internal imports.
