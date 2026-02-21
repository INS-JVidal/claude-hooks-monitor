# Step 2: Extract Shared Type into `internal/hookevt`

**Status:** Completed

## Goal

Create `internal/hookevt/hookevt.go` with the `HookEvent` type that both `package main` and `package tui` will import. This breaks the import cycle cleanly.

## Why `internal/`?

Go's `internal` directory convention prevents external packages from importing this type — it's only for use within this module. Without this, `tui/` cannot reference `HookEvent` because importing `package main` is not allowed in Go.

## New File

```go
// internal/hookevt/hookevt.go
package hookevt

import "time"

type HookEvent struct {
    HookType  string                 `json:"hook_type"`
    Timestamp time.Time              `json:"timestamp"`
    Data      map[string]interface{} `json:"data"`
}
```

## Changes to `main.go`

- Remove local `HookEvent` struct definition
- Add import: `"claude-hooks-monitor/internal/hookevt"`
- Replace all `HookEvent` references with `hookevt.HookEvent`

## Verification

- `go build .` succeeds
- No behavior changes
- Note: No Go unit tests exist yet; `test-hooks.sh` (bash integration tests) is unaffected by this change
