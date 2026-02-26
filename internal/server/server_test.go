package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-hooks-monitor/internal/filecache"
	"claude-hooks-monitor/internal/hookevt"
	"claude-hooks-monitor/internal/monitor"
)

// --- helpers ---

func newMonitor() *monitor.HookMonitor {
	return monitor.NewHookMonitor(nil)
}

func newMonitorWithCh() (*monitor.HookMonitor, chan hookevt.HookEvent) {
	ch := make(chan hookevt.HookEvent, 256)
	return monitor.NewHookMonitor(ch), ch
}

func makeNestedJSON(depth int) []byte {
	var b bytes.Buffer
	for i := 0; i < depth; i++ {
		b.WriteString(`{"a":`)
	}
	b.WriteString(`1`)
	for i := 0; i < depth; i++ {
		b.WriteString(`}`)
	}
	return b.Bytes()
}

func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Errorf("status = %d, want %d; body = %s", rr.Code, want, rr.Body.String())
	}
}

func assertJSON(t *testing.T, rr *httptest.ResponseRecorder, key string, want interface{}) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v; body = %s", err, rr.Body.String())
	}
	got, ok := m[key]
	if !ok {
		t.Fatalf("key %q missing from response JSON: %s", key, rr.Body.String())
	}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("JSON[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}

func postHook(handler http.HandlerFunc, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/hook/PreToolUse", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// ============================================================
// HandleHook — Happy Path
// ============================================================

func TestHandleHook_ValidJSON(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	rr := postHook(h, []byte(`{"key":"value"}`))
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "status", "ok")
	assertJSON(t, rr, "hook", "PreToolUse")
}

func TestHandleHook_EmptyBody(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	rr := postHook(h, nil)
	assertStatus(t, rr, 200)
	events := mon.GetEvents(1)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if len(events[0].Data) != 0 {
		t.Errorf("expected empty Data map, got %v", events[0].Data)
	}
}

func TestHandleHook_EventStored(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "SessionStart")
	postHook(h, []byte(`{"session":"abc"}`))
	events := mon.GetEvents(1)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].HookType != "SessionStart" {
		t.Errorf("HookType = %q, want %q", events[0].HookType, "SessionStart")
	}
	if events[0].Data["session"] != "abc" {
		t.Errorf("Data[session] = %v, want %q", events[0].Data["session"], "abc")
	}
}

func TestHandleHook_ResponseContentType(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	rr := postHook(h, []byte(`{}`))
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// ============================================================
// HandleHook — Error Paths
// ============================================================

func TestHandleHook_WrongMethods(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/hook/PreToolUse", nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			assertStatus(t, rr, 405)
		})
	}
}

func TestHandleHook_InvalidJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"broken", `{broken`},
		{"array", `[1,2,3]`},
		{"string", `"hello"`},
		{"number", `42`},
		{"trailing_comma", `{"a":1,}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mon := newMonitor()
			h := HandleHook(mon, "PreToolUse")
			rr := postHook(h, []byte(tc.body))
			assertStatus(t, rr, 400)
		})
	}
}

// ============================================================
// HandleHook — Security & Edge Cases
// ============================================================

func TestHandleHook_JSONDepthExceeded(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	rr := postHook(h, makeNestedJSON(101))
	assertStatus(t, rr, 400)
	if !strings.Contains(rr.Body.String(), "depth") {
		t.Errorf("expected error to mention depth, got: %s", rr.Body.String())
	}
}

func TestHandleHook_JSONDepthBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		depth int
		want  int
	}{
		{"depth_99", 99, 200},
		{"depth_100", 100, 200},
		{"depth_101", 101, 400},
		{"depth_200", 200, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mon := newMonitor()
			h := HandleHook(mon, "PreToolUse")
			rr := postHook(h, makeNestedJSON(tc.depth))
			assertStatus(t, rr, tc.want)
		})
	}
}

func TestHandleHook_BodySizeLimit(t *testing.T) {
	t.Parallel()
	// Exactly 1 MiB of valid JSON
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")

	// Build a JSON object that fits within 1 MiB
	// {"data":"AAAA...AAA"} where the value fills up to 1 MiB
	prefix := `{"data":"`
	suffix := `"}`
	padLen := (1 << 20) - len(prefix) - len(suffix)
	body := prefix + strings.Repeat("A", padLen) + suffix
	rr := postHook(h, []byte(body))
	assertStatus(t, rr, 200)
}

func TestHandleHook_BodyOverLimit(t *testing.T) {
	t.Parallel()
	// 1 MiB + 1KB — the LimitReader truncates, so JSON will be invalid
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	prefix := `{"data":"`
	suffix := `"}`
	padLen := (1 << 20) + 1024 - len(prefix) - len(suffix)
	body := prefix + strings.Repeat("A", padLen) + suffix
	rr := postHook(h, []byte(body))
	// The LimitReader truncates, making JSON invalid → 400
	assertStatus(t, rr, 400)
}

func TestHandleHook_BinaryPayload(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	rr := postHook(h, []byte{0x00, 0xFF, 0xFE, 0x01, 0x89, 0x50})
	assertStatus(t, rr, 400)
}

func TestHandleHook_NullBytesInJSON(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	// JSON allows \u0000 in string values
	rr := postHook(h, []byte(`{"val":"hello\u0000world"}`))
	assertStatus(t, rr, 200)
}

func TestHandleHook_UnicodePayload(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	body := `{"cjk":"日本語","emoji":"😀🎉","rtl":"مرحبا","mixed":"café"}`
	rr := postHook(h, []byte(body))
	assertStatus(t, rr, 200)
	events := mon.GetEvents(1)
	if events[0].Data["cjk"] != "日本語" {
		t.Errorf("CJK data lost: got %v", events[0].Data["cjk"])
	}
}

func TestHandleHook_DeepClone(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	postHook(h, []byte(`{"nested":{"inner":"original"}}`))

	events := mon.GetEvents(1)
	// Verify the nested value
	nested, ok := events[0].Data["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("expected nested map")
	}
	if nested["inner"] != "original" {
		t.Errorf("nested.inner = %v, want 'original'", nested["inner"])
	}
}

func TestHandleHook_LargeNumberOfKeys(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")

	// Build JSON with many keys (staying under 1 MiB)
	var b bytes.Buffer
	b.WriteString("{")
	for i := 0; i < 1000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"key%d":%d`, i, i)
	}
	b.WriteString("}")
	rr := postHook(h, b.Bytes())
	assertStatus(t, rr, 200)
}

// ============================================================
// HandleStats
// ============================================================

func TestHandleStats_Empty(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleStats(mon)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "total_hooks", float64(0))
}

func TestHandleStats_AfterEvents(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	for i := 0; i < 5; i++ {
		mon.AddEvent(hookevt.HookEvent{HookType: "PreToolUse", Timestamp: time.Now()})
	}
	for i := 0; i < 3; i++ {
		mon.AddEvent(hookevt.HookEvent{HookType: "SessionStart", Timestamp: time.Now()})
	}

	h := HandleStats(mon)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "total_hooks", float64(8))

	var m map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &m)
	stats := m["stats"].(map[string]interface{})
	if stats["PreToolUse"] != float64(5) {
		t.Errorf("PreToolUse count = %v, want 5", stats["PreToolUse"])
	}
	if stats["SessionStart"] != float64(3) {
		t.Errorf("SessionStart count = %v, want 3", stats["SessionStart"])
	}
}

func TestHandleStats_WrongMethod(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleStats(mon)
	req := httptest.NewRequest(http.MethodPost, "/stats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 405)
}

func TestHandleStats_DroppedCount(t *testing.T) {
	t.Parallel()
	ch := make(chan hookevt.HookEvent, 1) // tiny channel
	mon := monitor.NewHookMonitor(ch)

	// Fill the channel
	mon.AddEvent(hookevt.HookEvent{HookType: "A", Timestamp: time.Now()})
	// This should be dropped
	mon.AddEvent(hookevt.HookEvent{HookType: "B", Timestamp: time.Now()})

	h := HandleStats(mon)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "dropped", float64(1))
}

// ============================================================
// HandleEvents
// ============================================================

func TestHandleEvents_Empty(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleEvents(mon)
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "count", float64(0))
}

func TestHandleEvents_DefaultLimit(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	for i := 0; i < 150; i++ {
		mon.AddEvent(hookevt.HookEvent{HookType: "PreToolUse", Timestamp: time.Now()})
	}
	h := HandleEvents(mon)
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "count", float64(100)) // default limit
}

func TestHandleEvents_CustomLimit(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	for i := 0; i < 50; i++ {
		mon.AddEvent(hookevt.HookEvent{HookType: "PreToolUse", Timestamp: time.Now()})
	}
	h := HandleEvents(mon)
	req := httptest.NewRequest(http.MethodGet, "/events?limit=5", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "count", float64(5))
}

func TestHandleEvents_LimitEdgeCases(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	for i := 0; i < 50; i++ {
		mon.AddEvent(hookevt.HookEvent{HookType: "PreToolUse", Timestamp: time.Now()})
	}

	cases := []struct {
		name  string
		query string
		want  float64
	}{
		{"zero", "?limit=0", 50},           // limit=0 → returns all
		{"negative", "?limit=-1", 50},       // negative → returns all
		{"over_max", "?limit=9999", 50},     // capped but only 50 events exist
		{"not_a_number", "?limit=abc", 50},  // invalid → default 100, but only 50 events
		{"float", "?limit=3.14", 50},        // invalid → default 100, but only 50 events
		{"valid_small", "?limit=3", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := HandleEvents(mon)
			req := httptest.NewRequest(http.MethodGet, "/events"+tc.query, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			assertStatus(t, rr, 200)
			assertJSON(t, rr, "count", tc.want)
		})
	}
}

func TestHandleEvents_WrongMethod(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleEvents(mon)
	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 405)
}

// ============================================================
// HandleHealth
// ============================================================

func TestHandleHealth_OK(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	HandleHealth(rr, req)
	assertStatus(t, rr, 200)
	assertJSON(t, rr, "status", "healthy")
}

func TestHandleHealth_WrongMethod(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rr := httptest.NewRecorder()
	HandleHealth(rr, req)
	assertStatus(t, rr, 405)
}

func TestHandleHealth_ResponseFormat(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	HandleHealth(rr, req)

	var m map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &m)
	timeStr, ok := m["time"].(string)
	if !ok {
		t.Fatal("time field missing or not a string")
	}
	if _, err := time.Parse(time.RFC3339, timeStr); err != nil {
		t.Errorf("time %q is not valid RFC3339: %v", timeStr, err)
	}
}

// ============================================================
// SecurityHeaders Middleware
// ============================================================

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing Cache-Control: no-store")
	}
}

// ============================================================
// AuthMiddleware
// ============================================================

func TestAuth_ValidToken(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := AuthMiddleware("secret-token", inner)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
}

func TestAuth_Rejection(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := AuthMiddleware("secret-token", inner)

	cases := []struct {
		name   string
		header string
	}{
		{"wrong_token", "Bearer wrong-token"},
		{"missing_header", ""},
		{"empty_bearer", "Bearer "},
		{"no_bearer_prefix", "secret-token"},
		{"lowercase_bearer", "bearer secret-token"},
		{"basic_auth", "Basic c2VjcmV0LXRva2Vu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/stats", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assertStatus(t, rr, 401)
		})
	}
}

func TestAuth_HealthBypass(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := AuthMiddleware("secret-token", inner)

	// Without token — should still pass for /health
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
}

func TestAuth_HealthWithToken(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := AuthMiddleware("secret-token", inner)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assertStatus(t, rr, 200)
}

// ============================================================
// checkJSONDepth
// ============================================================

func TestCheckJSONDepth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		data    []byte
		depth   int
		wantErr bool
	}{
		{"flat_object", []byte(`{"a":1}`), 100, false},
		{"empty_object", []byte(`{}`), 100, false},
		{"empty_array", []byte(`[]`), 100, false},
		{"max_depth_ok", makeNestedJSON(100), 100, false},
		{"over_max_depth", makeNestedJSON(101), 100, true},
		{"mixed_depth_ok", []byte(`{"a":[{"b":[1]}]}`), 100, false},
		{"invalid_json", []byte(`{broken`), 100, false}, // defers to Unmarshal
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkJSONDepth(tc.data, tc.depth)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ============================================================
// cloneMap / cloneSlice
// ============================================================

func TestCloneMap_Nil(t *testing.T) {
	t.Parallel()
	if got := cloneMap(nil); got != nil {
		t.Errorf("cloneMap(nil) = %v, want nil", got)
	}
}

func TestCloneMap_Shallow(t *testing.T) {
	t.Parallel()
	orig := map[string]interface{}{"a": "b", "c": float64(1)}
	clone := cloneMap(orig)

	if clone["a"] != "b" || clone["c"] != float64(1) {
		t.Errorf("clone mismatch: %v", clone)
	}
	// Mutate original, verify clone is independent
	orig["a"] = "modified"
	if clone["a"] != "b" {
		t.Error("clone was affected by mutation of original")
	}
}

func TestCloneMap_DeepNested(t *testing.T) {
	t.Parallel()
	orig := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"value": "deep",
			},
		},
	}
	clone := cloneMap(orig)

	// Mutate deeply in original
	orig["level1"].(map[string]interface{})["level2"].(map[string]interface{})["value"] = "mutated"

	// Clone should be independent
	l1 := clone["level1"].(map[string]interface{})
	l2 := l1["level2"].(map[string]interface{})
	if l2["value"] != "deep" {
		t.Errorf("deep clone was affected by mutation: got %v", l2["value"])
	}
}

func TestCloneMap_WithSlice(t *testing.T) {
	t.Parallel()
	orig := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}
	clone := cloneMap(orig)

	origSlice := orig["items"].([]interface{})
	origSlice[0] = "mutated"

	cloneSlice := clone["items"].([]interface{})
	if cloneSlice[0] != "a" {
		t.Error("cloned slice was affected by mutation of original")
	}
}

func TestCloneSlice_Empty(t *testing.T) {
	t.Parallel()
	got := cloneSlice([]interface{}{})
	if got == nil || len(got) != 0 {
		t.Errorf("cloneSlice([]) = %v, want empty non-nil slice", got)
	}
}

func TestCloneSlice_NestedMaps(t *testing.T) {
	t.Parallel()
	orig := []interface{}{
		map[string]interface{}{"key": "val1"},
		map[string]interface{}{"key": "val2"},
	}
	clone := cloneSlice(orig)

	orig[0].(map[string]interface{})["key"] = "mutated"
	if clone[0].(map[string]interface{})["key"] != "val1" {
		t.Error("cloned slice map was affected by mutation")
	}
}

// ============================================================
// HandleHook — Additional edge cases for DuplicateKeys
// ============================================================

func TestHandleHook_DuplicateKeys(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	// Go's json.Unmarshal uses last-wins for duplicate keys
	rr := postHook(h, []byte(`{"a":1,"a":2}`))
	assertStatus(t, rr, 200)
	events := mon.GetEvents(1)
	if events[0].Data["a"] != float64(2) {
		t.Errorf("expected last-wins, got %v", events[0].Data["a"])
	}
}

// ============================================================
// Verify no event leak on error
// ============================================================

func TestHandleHook_NoEventOnError(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")

	// Invalid JSON should not store an event
	postHook(h, []byte(`{broken`))
	events := mon.GetEvents(10)
	if len(events) != 0 {
		t.Errorf("expected 0 events after invalid JSON, got %d", len(events))
	}

	// Depth exceeded should not store an event
	postHook(h, makeNestedJSON(101))
	events = mon.GetEvents(10)
	if len(events) != 0 {
		t.Errorf("expected 0 events after depth exceeded, got %d", len(events))
	}
}

// ============================================================
// Edge: Concurrent handler calls
// ============================================================

func TestHandleHook_Concurrent(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")

	const n = 100
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			body := fmt.Sprintf(`{"i":%d}`, i)
			req := httptest.NewRequest(http.MethodPost, "/hook/PreToolUse", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != 200 {
				t.Errorf("goroutine %d: status = %d", i, rr.Code)
			}
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}

	events := mon.GetEvents(n)
	if len(events) != n {
		t.Errorf("expected %d events, got %d", n, len(events))
	}
	stats := mon.GetStats()
	if stats["PreToolUse"] != n {
		t.Errorf("stats[PreToolUse] = %d, want %d", stats["PreToolUse"], n)
	}
}

// ============================================================
// Edge: Large payload that gets truncated by LimitReader
// ============================================================

func TestHandleHook_ExactlyMaxBody(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")

	// Build valid JSON that is exactly MaxBodyLen (1 MiB)
	key := `{"x":"`
	end := `"}`
	fill := strings.Repeat("Z", monitor.MaxBodyLen-len(key)-len(end))
	body := key + fill + end

	rr := postHook(h, []byte(body))
	// Should be 200 — exactly at limit
	assertStatus(t, rr, 200)
}

// ============================================================
// Read body error simulation
// ============================================================

type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestHandleHook_BodyReadError(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleHook(mon, "PreToolUse")
	req := httptest.NewRequest(http.MethodPost, "/hook/PreToolUse", errReader{})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 400)
	if !strings.Contains(rr.Body.String(), "failed to read body") {
		t.Errorf("expected body read error, got: %s", rr.Body.String())
	}
}

// ============================================================
// HandleCacheFile
// ============================================================

func TestHandleCacheFile_ValidLookup(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	fc := filecache.New()
	mon.SetFileCache(fc)
	fc.RecordRead("sess1", "/home/user/file.go", 1234567890, 4096)

	h := HandleCacheFile(mon)
	req := httptest.NewRequest(http.MethodGet, "/cache/file?session=sess1&path=/home/user/file.go", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assertStatus(t, rr, 200)

	var q filecache.CacheQuery
	json.NewDecoder(rr.Body).Decode(&q)
	if !q.Found {
		t.Fatal("expected found=true")
	}
	if q.MtimeNS != 1234567890 {
		t.Errorf("MtimeNS = %d, want 1234567890", q.MtimeNS)
	}
}

func TestHandleCacheFile_UnknownFile(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	fc := filecache.New()
	mon.SetFileCache(fc)

	h := HandleCacheFile(mon)
	req := httptest.NewRequest(http.MethodGet, "/cache/file?session=sess1&path=/unknown.go", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assertStatus(t, rr, 200)

	var q filecache.CacheQuery
	json.NewDecoder(rr.Body).Decode(&q)
	if q.Found {
		t.Error("expected found=false for unknown file")
	}
}

func TestHandleCacheFile_MissingParams(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	fc := filecache.New()
	mon.SetFileCache(fc)
	h := HandleCacheFile(mon)

	// Missing both
	req := httptest.NewRequest(http.MethodGet, "/cache/file", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 400)

	// Missing path
	req = httptest.NewRequest(http.MethodGet, "/cache/file?session=sess1", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 400)

	// Missing session
	req = httptest.NewRequest(http.MethodGet, "/cache/file?path=/file.go", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 400)
}

func TestHandleCacheFile_NilCache(t *testing.T) {
	t.Parallel()
	mon := newMonitor() // no SetFileCache

	h := HandleCacheFile(mon)
	req := httptest.NewRequest(http.MethodGet, "/cache/file?session=sess1&path=/file.go", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assertStatus(t, rr, 200)

	var result map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&result)
	if result["found"] != false {
		t.Errorf("expected found=false for nil cache, got %v", result["found"])
	}
}

func TestHandleCacheFile_WrongMethod(t *testing.T) {
	t.Parallel()
	mon := newMonitor()
	h := HandleCacheFile(mon)

	req := httptest.NewRequest(http.MethodPost, "/cache/file?session=s&path=p", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assertStatus(t, rr, 405)
}
