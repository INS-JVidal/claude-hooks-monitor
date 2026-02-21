# Step 4: Add Event Channel + Context to HookMonitor

**Status:** Completed + Review fixes applied

## Goal

Modify `HookMonitor` to optionally forward events to a channel (for TUI mode) instead of logging to console.

## Changes to `monitor.go`

```go
type HookMonitor struct {
    events  []hookevt.HookEvent
    mu      sync.RWMutex
    stats   map[string]int
    eventCh chan hookevt.HookEvent // nil when TUI inactive
    Dropped atomic.Int64           // Events dropped because TUI channel was full
}

func NewHookMonitor(eventCh chan hookevt.HookEvent) *HookMonitor { ... }

func (m *HookMonitor) AddEvent(event hookevt.HookEvent) {
    m.mu.Lock()
    // ... ring buffer + stats logic ...

    if m.eventCh != nil {
        // Non-blocking send — drop if TUI can't keep up
        select {
        case m.eventCh <- event:
        default:
            m.Dropped.Add(1)
        }
    }
    m.mu.Unlock()

    if m.eventCh == nil {
        logEvent(event)
    }
}
```

## Design Decisions

- **Non-blocking send**: Uses `select/default` so HTTP handlers never block waiting for the TUI to consume events. If the 256-buffer channel is full, events are dropped (they're still stored in the ring buffer and accessible via `/events` API).
- **Nil channel guard**: When `eventCh` is nil (console mode), falls back to the original `logEvent()` behavior. No behavior change in default mode.

## Review Fixes Applied

1. **Dropped event counter** — `Dropped atomic.Int64` tracks how many events were dropped because the TUI channel was full. The TUI header displays `"Dropped: N"` when non-zero, making silent drops visible to the user.
2. **Channel send inside lock** — The non-blocking channel send was moved inside the `m.mu.Lock()` critical section. Since `select/default` never blocks, there's no deadlock risk. This guarantees channel event order matches ring buffer insertion order under concurrent HTTP handlers.

## Verification

- `go build .` works
- Pass `nil` for eventCh in console mode — behavior unchanged
- TUI header shows dropped count when channel overflows
