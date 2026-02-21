package monitor

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"claude-hooks-monitor/internal/hookevt"

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

// AddEvent appends an event to the buffer (thread-safe, bounded).
// When eventCh is set, events are forwarded to the TUI channel instead of logged.
// The channel send happens after the lock is released — the event is already
// committed to the ring buffer so ordering is determined by append order.
//
// The event.Data map is shared by the ring buffer, the TUI channel consumer,
// and logEvent without synchronization beyond the initial lock. HandleHook
// deep-copies the parsed JSON before calling AddEvent, so the stored map is
// decoupled from the HTTP handler's local state.
func (m *HookMonitor) AddEvent(event hookevt.HookEvent) {
	m.mu.Lock()
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
	canSend := m.eventCh != nil && !m.chClosed
	m.mu.Unlock()

	if canSend {
		// Non-blocking send — drop if TUI can't keep up.
		select {
		case m.eventCh <- event:
		default:
			m.Dropped.Add(1)
		}
	} else if m.eventCh == nil {
		logEvent(event)
	}
	// If chClosed && eventCh != nil: channel is closed, skip both send and log.
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

// hookColor returns the pre-computed color printer for a given hook type.
func hookColor(hookType string) *color.Color {
	if c, ok := hookColors[hookType]; ok {
		return c
	}
	return defaultColor
}
