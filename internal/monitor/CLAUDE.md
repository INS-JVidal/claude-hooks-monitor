# monitor — Core event storage and console logging

All files stable — prefer this summary over reading source files.

## monitor.go

```go
const MaxEvents = 1000
const MaxBodyLen = 1 << 20

type HookMonitor struct {
    Dropped atomic.Int64
    // unexported: events, mu, stats, eventCh, chClosed, eventSink, fileCache
}

func NewHookMonitor(eventCh chan hookevt.HookEvent) *HookMonitor
func (m *HookMonitor) AddEvent(event hookevt.HookEvent)
func (m *HookMonitor) CloseChannel()
func (m *HookMonitor) SetSink(s sink.EventSink)
func (m *HookMonitor) CloseSink()
func (m *HookMonitor) SetFileCache(fc *filecache.SessionFileCache)
func (m *HookMonitor) FileCache() *filecache.SessionFileCache
func (m *HookMonitor) GetStats() map[string]int
func (m *HookMonitor) GetEvents(limit int) []hookevt.HookEvent
```

Bounded ring buffer (max 1000, trims to 900). AddEvent: appends under lock, non-blocking channel send to TUI (or logEvent in console mode), fire-and-forget goroutine to sink, file cache processing (PostToolUse Read _cache → RecordRead, SessionEnd → EndSession). CloseChannel prevents send-on-closed-channel panic via chClosed flag under lock.

Concurrency: sync.RWMutex (events/stats), atomic.Int64 (Dropped), sync.Mutex (logMu for console output serialization). Channel send inside lock to prevent TOCTOU with CloseChannel.

## monitor_test.go

Tests for AddEvent, GetStats, GetEvents, concurrent access, ring buffer trim.

Imports: `filecache` (SessionFileCache), `hookevt` (HookEvent), `sink` (EventSink).
