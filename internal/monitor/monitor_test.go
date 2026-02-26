package monitor

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"claude-hooks-monitor/internal/filecache"
	"claude-hooks-monitor/internal/hookevt"
)

func makeEvent(hookType string, i int) hookevt.HookEvent {
	return hookevt.HookEvent{
		HookType:  hookType,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"i": float64(i)},
	}
}

// ============================================================
// Ring Buffer Behavior
// ============================================================

func TestAddEvent_Basic(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	mon.AddEvent(makeEvent("PreToolUse", 1))

	events := mon.GetEvents(10)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].HookType != "PreToolUse" {
		t.Errorf("HookType = %q, want PreToolUse", events[0].HookType)
	}
}

func TestAddEvent_OrderPreserved(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < 10; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}

	events := mon.GetEvents(10)
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}
	for i, e := range events {
		got := e.Data["i"].(float64)
		if int(got) != i {
			t.Errorf("event[%d].i = %v, want %d", i, got, i)
		}
	}
}

func TestAddEvent_MaxCapacity(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < MaxEvents; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}

	events := mon.GetEvents(MaxEvents + 100)
	if len(events) != MaxEvents {
		t.Errorf("expected %d events, got %d", MaxEvents, len(events))
	}
}

func TestAddEvent_TrimAt1001(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < MaxEvents+1; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}

	events := mon.GetEvents(MaxEvents + 100)
	if len(events) != trimTarget {
		t.Errorf("expected %d events after trim, got %d", trimTarget, len(events))
	}
}

func TestAddEvent_TrimPreservesNewest(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	total := MaxEvents + 1
	for i := 0; i < total; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}

	events := mon.GetEvents(trimTarget)
	// After trim, should have events from (total-trimTarget) to (total-1)
	firstExpected := total - trimTarget
	first := events[0].Data["i"].(float64)
	if int(first) != firstExpected {
		t.Errorf("first event i = %v, want %d", first, firstExpected)
	}
	last := events[len(events)-1].Data["i"].(float64)
	if int(last) != total-1 {
		t.Errorf("last event i = %v, want %d", last, total-1)
	}
}

func TestAddEvent_TrimMultiple(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	// Add 2000 events (triggers trim multiple times)
	for i := 0; i < 2000; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}

	events := mon.GetEvents(MaxEvents + 100)
	if len(events) > MaxEvents {
		t.Errorf("events should never exceed MaxEvents, got %d", len(events))
	}
	// Last event should be i=1999
	last := events[len(events)-1].Data["i"].(float64)
	if int(last) != 1999 {
		t.Errorf("last event i = %v, want 1999", last)
	}
}

// ============================================================
// GetEvents edge cases
// ============================================================

func TestGetEvents_LimitZero(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < 5; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}
	events := mon.GetEvents(0)
	if len(events) != 5 {
		t.Errorf("GetEvents(0) returned %d events, want 5 (all)", len(events))
	}
}

func TestGetEvents_LimitNegative(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < 5; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}
	events := mon.GetEvents(-1)
	if len(events) != 5 {
		t.Errorf("GetEvents(-1) returned %d events, want 5 (all)", len(events))
	}
}

func TestGetEvents_LimitExceedsCount(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < 10; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}
	events := mon.GetEvents(500)
	if len(events) != 10 {
		t.Errorf("GetEvents(500) with 10 events returned %d, want 10", len(events))
	}
}

func TestGetEvents_Empty(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	events := mon.GetEvents(10)
	if len(events) != 0 {
		t.Errorf("GetEvents on empty monitor returned %d events", len(events))
	}
}

func TestGetEvents_ReturnsIndependentSlice(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	mon.AddEvent(makeEvent("PreToolUse", 1))
	mon.AddEvent(makeEvent("PreToolUse", 2))

	events := mon.GetEvents(2)
	// Mutate the returned slice
	events[0] = hookevt.HookEvent{HookType: "Mutated"}

	// Original should be unaffected
	original := mon.GetEvents(2)
	if original[0].HookType != "PreToolUse" {
		t.Error("modifying returned slice affected monitor's internal state")
	}
}

// ============================================================
// Stats
// ============================================================

func TestGetStats_Empty(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	stats := mon.GetStats()
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %v", stats)
	}
}

func TestGetStats_Counting(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	for i := 0; i < 5; i++ {
		mon.AddEvent(makeEvent("PreToolUse", i))
	}
	for i := 0; i < 3; i++ {
		mon.AddEvent(makeEvent("SessionStart", i))
	}

	stats := mon.GetStats()
	if stats["PreToolUse"] != 5 {
		t.Errorf("PreToolUse = %d, want 5", stats["PreToolUse"])
	}
	if stats["SessionStart"] != 3 {
		t.Errorf("SessionStart = %d, want 3", stats["SessionStart"])
	}
}

func TestGetStats_ReturnsIndependentMap(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	mon.AddEvent(makeEvent("PreToolUse", 1))

	stats := mon.GetStats()
	stats["PreToolUse"] = 999

	original := mon.GetStats()
	if original["PreToolUse"] != 1 {
		t.Error("modifying returned stats affected monitor's internal state")
	}
}

// ============================================================
// TUI Channel Management
// ============================================================

func TestChannel_EventForwarded(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 10)
	mon := NewHookMonitor(ch)

	mon.AddEvent(makeEvent("PreToolUse", 42))

	select {
	case evt := <-ch:
		if evt.HookType != "PreToolUse" {
			t.Errorf("forwarded event HookType = %q, want PreToolUse", evt.HookType)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event on channel")
	}
}

func TestChannel_DroppedWhenFull(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 2)
	mon := NewHookMonitor(ch)

	// Fill the channel
	mon.AddEvent(makeEvent("A", 1))
	mon.AddEvent(makeEvent("A", 2))

	// This should be dropped (channel full, non-blocking send)
	mon.AddEvent(makeEvent("A", 3))

	if mon.Dropped.Load() != 1 {
		t.Errorf("Dropped = %d, want 1", mon.Dropped.Load())
	}
}

func TestChannel_NilChannel(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	// Should not panic
	mon.AddEvent(makeEvent("PreToolUse", 1))
	events := mon.GetEvents(1)
	if len(events) != 1 {
		t.Errorf("expected 1 event with nil channel, got %d", len(events))
	}
}

func TestCloseChannel_Idempotent(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 10)
	mon := NewHookMonitor(ch)

	// Should not panic on double close
	mon.CloseChannel()
	mon.CloseChannel()
}

func TestCloseChannel_DropsAfterClose(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 10)
	mon := NewHookMonitor(ch)

	mon.CloseChannel()
	mon.AddEvent(makeEvent("PreToolUse", 1))

	if mon.Dropped.Load() != 1 {
		t.Errorf("Dropped = %d after close, want 1", mon.Dropped.Load())
	}

	// Event should still be stored in the ring buffer
	events := mon.GetEvents(1)
	if len(events) != 1 {
		t.Error("event should still be stored in buffer after channel close")
	}
}

func TestCloseChannel_NilChannel(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	// Should not panic
	mon.CloseChannel()
}

// ============================================================
// Concurrency (run with -race)
// ============================================================

func TestConcurrent_AddEvent(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	const goroutines = 100
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				mon.AddEvent(makeEvent("PreToolUse", g*eventsPerGoroutine+i))
			}
		}(g)
	}
	wg.Wait()

	stats := mon.GetStats()
	total := goroutines * eventsPerGoroutine
	if stats["PreToolUse"] != total {
		t.Errorf("stats[PreToolUse] = %d, want %d", stats["PreToolUse"], total)
	}
}

func TestConcurrent_AddAndRead(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	const n = 500

	var wg sync.WaitGroup

	// Writers
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				mon.AddEvent(makeEvent("PreToolUse", i))
			}
		}()
	}

	// Readers
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				mon.GetEvents(10)
				mon.GetStats()
			}
		}()
	}

	wg.Wait()

	// Just verify no panic and data is consistent
	stats := mon.GetStats()
	if stats["PreToolUse"] != 10*n {
		t.Errorf("stats total = %d, want %d", stats["PreToolUse"], 10*n)
	}
}

func TestConcurrent_AddAndClose(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 256)
	mon := NewHookMonitor(ch)

	var wg sync.WaitGroup

	// Writer goroutines
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				mon.AddEvent(makeEvent("PreToolUse", i))
			}
		}()
	}

	// Close channel midway through
	go func() {
		time.Sleep(time.Millisecond)
		mon.CloseChannel()
	}()

	wg.Wait()

	// Verify no panic, stats consistent
	stats := mon.GetStats()
	if stats["PreToolUse"] != 5000 {
		t.Errorf("stats total = %d, want 5000", stats["PreToolUse"])
	}

	// Dropped should be >= 0 (some events may have been dropped after close)
	dropped := mon.Dropped.Load()
	t.Logf("dropped = %d (informational)", dropped)
}

func TestConcurrent_StressDropped(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 1) // tiny channel
	mon := NewHookMonitor(ch)

	const goroutines = 50
	const eventsPerGoroutine = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				mon.AddEvent(makeEvent("PreToolUse", i))
			}
		}()
	}
	wg.Wait()

	total := goroutines * eventsPerGoroutine
	stats := mon.GetStats()
	sent := stats["PreToolUse"]

	// All events should be counted in stats
	if sent != total {
		t.Errorf("stats total = %d, want %d", sent, total)
	}

	// Dropped should account for events that couldn't be sent to channel
	dropped := mon.Dropped.Load()
	// At least some should be dropped since channel is tiny
	if dropped == 0 {
		t.Log("warning: no events dropped despite tiny channel (possible but unlikely)")
	}
	t.Logf("total=%d, dropped=%d (channel capacity=1)", total, dropped)
}

// ============================================================
// NewHookMonitor initialization
// ============================================================

func TestNewHookMonitor_InitialState(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	if events := mon.GetEvents(10); len(events) != 0 {
		t.Errorf("new monitor has %d events, want 0", len(events))
	}
	if stats := mon.GetStats(); len(stats) != 0 {
		t.Errorf("new monitor has stats %v, want empty", stats)
	}
	if mon.Dropped.Load() != 0 {
		t.Errorf("new monitor Dropped = %d, want 0", mon.Dropped.Load())
	}
}

// ============================================================
// Stats after trim (ensures stats survive buffer trimming)
// ============================================================

func TestStats_SurviveTrim(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	total := MaxEvents + 500
	for i := 0; i < total; i++ {
		hookType := "PreToolUse"
		if i%3 == 0 {
			hookType = "SessionStart"
		}
		mon.AddEvent(hookevt.HookEvent{
			HookType:  hookType,
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"i": float64(i)},
		})
	}

	stats := mon.GetStats()
	totalStats := 0
	for _, v := range stats {
		totalStats += v
	}
	if totalStats != total {
		t.Errorf("total stats = %d, want %d (stats should not be affected by trim)", totalStats, total)
	}

	// But events count should be bounded
	events := mon.GetEvents(MaxEvents + 100)
	if len(events) > MaxEvents {
		t.Errorf("events count = %d, should be <= %d", len(events), MaxEvents)
	}
}

// ============================================================
// Multiple hook types
// ============================================================

func TestMultipleHookTypes(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)

	types := []string{"SessionStart", "PreToolUse", "PostToolUse", "Notification", "Stop"}
	for _, ht := range types {
		for i := 0; i < 10; i++ {
			mon.AddEvent(hookevt.HookEvent{
				HookType:  ht,
				Timestamp: time.Now(),
				Data:      map[string]interface{}{"type": ht, "i": float64(i)},
			})
		}
	}

	stats := mon.GetStats()
	for _, ht := range types {
		if stats[ht] != 10 {
			t.Errorf("stats[%s] = %d, want 10", ht, stats[ht])
		}
	}

	events := mon.GetEvents(50)
	if len(events) != 50 {
		t.Errorf("expected 50 events, got %d", len(events))
	}
}

// ============================================================
// Dropped counter atomicity
// ============================================================

func TestDropped_ConcurrentIncrement(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent) // unbuffered — every send blocks
	mon := NewHookMonitor(ch)

	const n = 1000
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mon.AddEvent(makeEvent(fmt.Sprintf("Type%d", i%5), i))
		}(i)
	}
	wg.Wait()

	// With unbuffered channel and no reader, all events should be dropped
	if mon.Dropped.Load() != n {
		t.Errorf("Dropped = %d, want %d", mon.Dropped.Load(), n)
	}

	// But all events should still be in the buffer
	stats := mon.GetStats()
	total := 0
	for _, v := range stats {
		total += v
	}
	if total != n {
		t.Errorf("total stats = %d, want %d", total, n)
	}
}

// ============================================================
// File Cache Integration (processFileCache)
// ============================================================

func TestFileCache_PostToolUseRead(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess1",
			"_cache": map[string]interface{}{
				"file_path": "/home/user/handler.go",
				"mtime_ns":  float64(1234567890),
				"size":      float64(4096),
			},
		},
	})

	q := fc.Lookup("sess1", "/home/user/handler.go")
	if !q.Found {
		t.Fatal("expected file to be cached after PostToolUse Read")
	}
	if q.MtimeNS != 1234567890 {
		t.Errorf("MtimeNS = %d, want 1234567890", q.MtimeNS)
	}
	if q.Size != 4096 {
		t.Errorf("Size = %d, want 4096", q.Size)
	}
}

func TestFileCache_PostToolUseRead_NoCache(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess1",
		},
	})

	q := fc.Lookup("sess1", "/any/file.go")
	if q.Found {
		t.Error("expected no cache entry without _cache metadata")
	}
}

func TestFileCache_PostToolUseRead_NonReadTool(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Write",
			"session_id": "sess1",
			"_cache": map[string]interface{}{
				"file_path": "/home/user/file.go",
				"mtime_ns":  float64(100),
				"size":      float64(200),
			},
		},
	})

	q := fc.Lookup("sess1", "/home/user/file.go")
	if q.Found {
		t.Error("expected no cache entry for non-Read tool")
	}
}

func TestFileCache_PostToolUseRead_MissingSessionID(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name": "Read",
			"_cache": map[string]interface{}{
				"file_path": "/home/user/file.go",
				"mtime_ns":  float64(100),
				"size":      float64(200),
			},
		},
	})

	q := fc.Lookup("", "/home/user/file.go")
	if q.Found {
		t.Error("expected no cache entry without session_id")
	}
}

func TestFileCache_PostToolUseRead_ZeroMtime(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess1",
			"_cache": map[string]interface{}{
				"file_path": "/home/user/file.go",
				"mtime_ns":  float64(0),
				"size":      float64(100),
			},
		},
	})

	q := fc.Lookup("sess1", "/home/user/file.go")
	if q.Found {
		t.Error("expected no cache entry with zero mtime")
	}
}

func TestFileCache_SessionEnd(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess1",
			"_cache": map[string]interface{}{
				"file_path": "/home/user/file.go",
				"mtime_ns":  float64(1234567890),
				"size":      float64(100),
			},
		},
	})

	q := fc.Lookup("sess1", "/home/user/file.go")
	if !q.Found {
		t.Fatal("expected file to be cached")
	}

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "SessionEnd",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"session_id": "sess1"},
	})

	q = fc.Lookup("sess1", "/home/user/file.go")
	if q.Found {
		t.Error("expected cache to be cleared after SessionEnd")
	}
}

func TestFileCache_SessionEnd_NoSessionID(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	// Should not panic
	mon.AddEvent(hookevt.HookEvent{
		HookType:  "SessionEnd",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{},
	})
}

func TestFileCache_OtherHookTypes_Ignored(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PreToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess1",
			"_cache": map[string]interface{}{
				"file_path": "/file.go",
				"mtime_ns":  float64(100),
				"size":      float64(200),
			},
		},
	})

	q := fc.Lookup("sess1", "/file.go")
	if q.Found {
		t.Error("PreToolUse should not populate file cache")
	}
}

func TestFileCache_NilCache_NoError(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)

	// Should not panic with nil file cache
	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess1",
			"_cache": map[string]interface{}{
				"file_path": "/file.go",
				"mtime_ns":  float64(100),
				"size":      float64(200),
			},
		},
	})
}

func TestFileCache_PostToolUseRead_JSONNumber(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)
	fc := filecache.New()
	mon.SetFileCache(fc)

	// When UseNumber() is enabled, JSON numbers arrive as json.Number.
	// jsonInt64 must handle both json.Number and float64.
	mon.AddEvent(hookevt.HookEvent{
		HookType:  "PostToolUse",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"tool_name":  "Read",
			"session_id": "sess-jn",
			"_cache": map[string]interface{}{
				"file_path": "/home/user/big_mtime.go",
				"mtime_ns":  json.Number("1772138832089257486"),
				"size":      json.Number("4096"),
			},
		},
	})

	q := fc.Lookup("sess-jn", "/home/user/big_mtime.go")
	if !q.Found {
		t.Fatal("expected file to be cached with json.Number values")
	}
	if q.MtimeNS != 1772138832089257486 {
		t.Errorf("MtimeNS = %d, want 1772138832089257486 (exact int64)", q.MtimeNS)
	}
	if q.Size != 4096 {
		t.Errorf("Size = %d, want 4096", q.Size)
	}
}

func TestFileCache_SetAndGet(t *testing.T) {
	t.Parallel()
	mon := NewHookMonitor(nil)

	if mon.FileCache() != nil {
		t.Error("FileCache should be nil before SetFileCache")
	}

	fc := filecache.New()
	mon.SetFileCache(fc)

	if mon.FileCache() != fc {
		t.Error("FileCache should return the set cache")
	}
}
