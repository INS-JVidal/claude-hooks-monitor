package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claude-hooks-monitor/internal/filecache"
	"claude-hooks-monitor/internal/hookevt"
	"claude-hooks-monitor/internal/sink"

	"github.com/fatih/color"
)

const (
	MaxEvents  = 1000
	trimTarget = MaxEvents - 100 // After trim, keep the last 900 events.
	MaxBodyLen = 1 << 20         // 1 MiB — generous limit for hook payloads.
)

// separator is pre-computed once to avoid re-allocating on every event.
var separator = strings.Repeat("═", 80)

// hookColors maps hook types to pre-computed color printers.
var hookColors = map[string]*color.Color{
	"SessionStart":       color.New(color.FgGreen, color.Bold),
	"SessionEnd":         color.New(color.FgRed, color.Bold),
	"PreToolUse":         color.New(color.FgYellow, color.Bold),
	"PostToolUse":        color.New(color.FgCyan, color.Bold),
	"PostToolUseFailure": color.New(color.FgHiRed, color.Bold),
	"UserPromptSubmit":   color.New(color.FgMagenta, color.Bold),
	"Notification":       color.New(color.FgBlue, color.Bold),
	"PermissionRequest":  color.New(color.FgWhite, color.Bold),
	"Stop":               color.New(color.FgRed),
	"SubagentStart":      color.New(color.FgHiCyan),
	"SubagentStop":       color.New(color.FgHiCyan, color.Bold),
	"TeammateIdle":       color.New(color.FgHiBlue),
	"TaskCompleted":      color.New(color.FgHiGreen),
	"ConfigChange":       color.New(color.FgHiYellow),
	"PreCompact":         color.New(color.FgHiMagenta),
}

var defaultColor = color.New(color.FgWhite)

// logMu serializes terminal output from concurrent logEvent calls
// to prevent interleaved/garbled log lines in console mode.
var logMu sync.Mutex

// HookMonitor stores hook events in a bounded ring buffer with thread-safe access.
type HookMonitor struct {
	events    []hookevt.HookEvent
	mu        sync.RWMutex
	stats     map[string]int
	eventCh   chan hookevt.HookEvent // nil when TUI inactive
	chClosed  bool                   // true after CloseChannel(); prevents send-on-closed-channel panic
	Dropped   atomic.Int64           // Events dropped because TUI channel was full
	eventSink sink.EventSink         // nil = no external forwarding
	fileCache *filecache.SessionFileCache // nil = file caching disabled
}

// NewHookMonitor returns an initialized HookMonitor.
// Pass a non-nil eventCh to forward events to the TUI instead of logging.
func NewHookMonitor(eventCh chan hookevt.HookEvent) *HookMonitor {
	return &HookMonitor{
		events:  make([]hookevt.HookEvent, 0, 256),
		stats:   make(map[string]int),
		eventCh: eventCh,
	}
}

// SetSink configures an optional EventSink for forwarding events to an
// external consumer (e.g., the hooks-store companion). Must be called
// before any events are added. Pass nil to disable forwarding.
func (m *HookMonitor) SetSink(s sink.EventSink) {
	m.eventSink = s
}

// SetFileCache configures the session file cache for tracking Read tool usage.
// Must be called before any events are added. Pass nil to disable caching.
func (m *HookMonitor) SetFileCache(fc *filecache.SessionFileCache) {
	m.fileCache = fc
}

// FileCache returns the configured file cache, or nil if caching is disabled.
func (m *HookMonitor) FileCache() *filecache.SessionFileCache {
	return m.fileCache
}

// CloseSink closes the event sink if one is configured.
// Safe to call even if no sink is set.
func (m *HookMonitor) CloseSink() {
	if m.eventSink != nil {
		m.eventSink.Close()
	}
}

// AddEvent appends an event to the buffer (thread-safe, bounded).
// When eventCh is set, events are forwarded to the TUI channel instead of logged.
//
// The non-blocking channel send happens inside the lock to prevent a TOCTOU race
// with CloseChannel: without this, a goroutine could read canSend=true, release the
// lock, and then attempt to send on a channel that CloseChannel closed in between —
// causing a panic. The select/default send never blocks, so holding the lock during
// the send has negligible contention impact.
//
// The event.Data map is shared by the ring buffer, the TUI channel consumer,
// and logEvent without synchronization beyond the initial lock. HandleHook
// deep-copies the parsed JSON before calling AddEvent, so the stored map is
// decoupled from the HTTP handler's local state.
func (m *HookMonitor) AddEvent(event hookevt.HookEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = append(m.events, event)
	// Trim oldest when exceeding max — copy into a fresh slice so the GC
	// can reclaim the old backing array (avoids the slice-pinning leak).
	if len(m.events) > MaxEvents {
		discard := len(m.events) - trimTarget
		fresh := make([]hookevt.HookEvent, trimTarget, MaxEvents)
		copy(fresh, m.events[discard:])
		m.events = fresh
	}
	m.stats[event.HookType]++

	// Process file cache updates (PostToolUse Read _cache, SessionEnd cleanup).
	// Runs under the existing write lock — all operations are O(1) map access.
	if m.fileCache != nil {
		m.processFileCache(event)
	}

	// Channel send MUST happen inside the lock to prevent send-on-closed-channel
	// panic. CloseChannel also acquires this lock before closing, so the send and
	// close can never race.
	switch {
	case m.eventCh != nil && !m.chClosed:
		select {
		case m.eventCh <- event:
		default:
			m.Dropped.Add(1)
		}
	case m.eventCh == nil:
		// Console mode — logEvent has its own logMu for serialization.
		// Holding mu during the fast buffered write has negligible contention
		// since there is no TUI channel consumer competing for the lock.
		logEvent(event)
	default:
		// Channel is closed (shutdown in progress) — count as dropped.
		m.Dropped.Add(1)
	}

	// Forward to external sink outside the lock. The goroutine captures a
	// copy of the event (value type with deep-copied Data map from HandleHook),
	// so no synchronization is needed beyond the initial read of m.eventSink.
	if m.eventSink != nil {
		evt := event
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = m.eventSink.Send(ctx, evt) // fire-and-forget
		}()
	}
}

// CloseChannel marks the TUI event channel as closed and closes it.
// Must be called instead of close(eventCh) directly to prevent AddEvent
// from sending on a closed channel (which panics in Go).
func (m *HookMonitor) CloseChannel() {
	m.mu.Lock()
	if m.eventCh != nil && !m.chClosed {
		m.chClosed = true
		close(m.eventCh)
	}
	m.mu.Unlock()
}

// logEvent prints a colorized event to the console.
// Builds the full output into a buffer, then writes atomically under logMu
// to prevent interleaving from concurrent HTTP handler goroutines.
func logEvent(event hookevt.HookEvent) {
	printer := hookColor(event.HookType)

	var buf strings.Builder
	buf.WriteString(separator)
	buf.WriteByte('\n')

	// Color-formatted hook type line.
	buf.WriteString(printer.Sprintf("  ⚡ %s\n", event.HookType))
	buf.WriteString(fmt.Sprintf("  🕐 %s\n", event.Timestamp.Format("15:04:05.000")))

	if jsonBytes, err := json.MarshalIndent(event.Data, "  ", "  "); err == nil {
		buf.WriteString("  ")
		buf.Write(jsonBytes)
		buf.WriteByte('\n')
	}
	buf.WriteString(separator)
	buf.WriteByte('\n')

	logMu.Lock()
	fmt.Print(buf.String())
	logMu.Unlock()
}

// GetStats returns a copy of the stats map (thread-safe).
func (m *HookMonitor) GetStats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]int, len(m.stats))
	for k, v := range m.stats {
		result[k] = v
	}
	return result
}

// GetEvents returns the last N events (thread-safe).
func (m *HookMonitor) GetEvents(limit int) []hookevt.HookEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	start := len(m.events) - limit
	result := make([]hookevt.HookEvent, limit)
	copy(result, m.events[start:])
	return result
}

// processFileCache handles file cache updates from hook events.
// Called under the write lock in AddEvent — must be fast (O(1) map ops only).
//
// PostToolUse with tool_name=="Read" and _cache metadata: records the file read.
// SessionEnd: cleans up session data to free memory.
func (m *HookMonitor) processFileCache(event hookevt.HookEvent) {
	switch event.HookType {
	case "PostToolUse":
		toolName, _ := event.Data["tool_name"].(string)
		if toolName != "Read" {
			return
		}
		cache, ok := event.Data["_cache"].(map[string]interface{})
		if !ok {
			return
		}
		filePath, _ := cache["file_path"].(string)
		if filePath == "" {
			return
		}
		mtimeNS, _ := cache["mtime_ns"].(float64) // JSON numbers are float64
		if mtimeNS == 0 {
			return // zero mtime indicates malformed cache metadata
		}
		size, _ := cache["size"].(float64)
		sessionID := extractSessionID(event.Data)
		if sessionID == "" {
			return
		}
		m.fileCache.RecordRead(sessionID, filePath, int64(mtimeNS), int64(size))

	case "SessionEnd":
		sessionID := extractSessionID(event.Data)
		if sessionID != "" {
			m.fileCache.EndSession(sessionID)
		}
	}
}

// extractSessionID pulls the session_id from event data.
// Claude Code provides this in the hook payload.
func extractSessionID(data map[string]interface{}) string {
	if id, ok := data["session_id"].(string); ok {
		return id
	}
	return ""
}

// hookColor returns the pre-computed color printer for a given hook type.
func hookColor(hookType string) *color.Color {
	if c, ok := hookColors[hookType]; ok {
		return c
	}
	return defaultColor
}
