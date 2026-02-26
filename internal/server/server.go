package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"claude-hooks-monitor/internal/hookevt"
	"claude-hooks-monitor/internal/monitor"
)

const maxJSONDepth = 100 // Reject payloads nested deeper than this.

// SecurityHeaders adds standard security response headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware enforces bearer token authentication when HOOK_MONITOR_TOKEN
// is set. The /health endpoint is exempt so monitoring tools can check liveness.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow /health without auth for liveness probes.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HandleHook returns an HTTP handler for a specific hook type.
func HandleHook(mon *monitor.HookMonitor, hookType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		body, err := io.ReadAll(io.LimitReader(r.Body, monitor.MaxBodyLen))
		if err != nil {
			http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
			return
		}

		var data map[string]interface{}
		if len(body) > 0 {
			if err := checkJSONDepth(body, maxJSONDepth); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
				return
			}
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.UseNumber() // Preserve int64 precision for nanosecond timestamps.
			if err := dec.Decode(&data); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
		} else {
			data = make(map[string]interface{})
		}

		event := hookevt.HookEvent{
			HookType:  hookType,
			Timestamp: time.Now(),
			Data:      cloneMap(data),
		}
		mon.AddEvent(event)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"hook":   hookType,
		}); err != nil {
			// Response partially written; log but can't change status code.
			return
		}
	}
}

// HandleStats returns aggregate hook statistics.
func HandleStats(mon *monitor.HookMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		stats := mon.GetStats()
		total := 0
		for _, v := range stats {
			total += v
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"stats":       stats,
			"total_hooks": total,
			"dropped":     mon.Dropped.Load(),
		}); err != nil {
			return
		}
	}
}

// HandleEvents returns the last N events.
func HandleEvents(mon *monitor.HookMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		limit := 100
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > monitor.MaxEvents {
			limit = monitor.MaxEvents
		}

		events := mon.GetEvents(limit)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"events":  events,
			"count":   len(events),
			"dropped": mon.Dropped.Load(),
		}); err != nil {
			return
		}
	}
}

// cloneMap creates a shallow copy of a map[string]interface{}.
// Nested maps and slices are cloned recursively so the returned map shares no
// mutable state with the original. This enforces the read-only invariant on
// event.Data — the HTTP handler's parsed JSON is decoupled from the stored event.
func cloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			out[k] = cloneMap(val)
		case []interface{}:
			out[k] = cloneSlice(val)
		default:
			out[k] = v // primitives (string, float64, bool, nil) are immutable
		}
	}
	return out
}

// cloneSlice creates a deep copy of a []interface{} from JSON unmarshal.
func cloneSlice(s []interface{}) []interface{} {
	out := make([]interface{}, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]interface{}:
			out[i] = cloneMap(val)
		case []interface{}:
			out[i] = cloneSlice(val)
		default:
			out[i] = v
		}
	}
	return out
}

// checkJSONDepth scans raw JSON tokens to reject payloads that exceed maxDepth
// nesting levels. This prevents stack exhaustion during json.Unmarshal and the
// subsequent recursive cloneMap/cloneSlice calls.
func checkJSONDepth(data []byte, maxDepth int) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		t, err := dec.Token()
		if err != nil {
			return nil // io.EOF or parse error — let Unmarshal handle it
		}
		switch t {
		case json.Delim('{'), json.Delim('['):
			depth++
			if depth > maxDepth {
				return fmt.Errorf("JSON nesting exceeds maximum depth of %d", maxDepth)
			}
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
}

// HandleCacheFile returns cached file metadata for a session.
// GET /cache/file?session=X&path=Y → CacheQuery JSON.
// Returns 400 if either query parameter is missing.
// Returns {"found":false} if the file cache is disabled or the file is unknown.
func HandleCacheFile(mon *monitor.HookMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.URL.Query().Get("session")
		filePath := r.URL.Query().Get("path")
		if sessionID == "" || filePath == "" {
			http.Error(w, `{"error":"session and path query parameters required"}`, http.StatusBadRequest)
			return
		}

		fc := mon.FileCache()
		if fc == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"found": false})
			return
		}

		q := fc.Lookup(sessionID, filePath)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(q); err != nil {
			return
		}
	}
}

// HandleHealth returns a simple health check response.
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	}); err != nil {
		return
	}
}
