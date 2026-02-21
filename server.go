package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"claude-hooks-monitor/internal/hookevt"
)

// securityHeaders adds standard security response headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// authMiddleware enforces bearer token authentication when HOOK_MONITOR_TOKEN
// is set. The /health endpoint is exempt so monitoring tools can check liveness.
func authMiddleware(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow /health without auth for liveness probes.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.EqualFold(r.Header.Get("Authorization"), expected) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleHook returns an HTTP handler for a specific hook type.
func handleHook(monitor *HookMonitor, hookType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyLen))
		if err != nil {
			http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
			return
		}

		var data map[string]interface{}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &data); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
		} else {
			data = make(map[string]interface{})
		}

		event := hookevt.HookEvent{
			HookType:  hookType,
			Timestamp: time.Now(),
			Data:      data,
		}
		monitor.AddEvent(event)

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

// handleStats returns aggregate hook statistics.
func handleStats(monitor *HookMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		stats := monitor.GetStats()
		total := 0
		for _, v := range stats {
			total += v
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"stats":       stats,
			"total_hooks": total,
		}); err != nil {
			return
		}
	}
}

// handleEvents returns the last N events.
func handleEvents(monitor *HookMonitor) http.HandlerFunc {
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
		if limit > maxEvents {
			limit = maxEvents
		}

		events := monitor.GetEvents(limit)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"events": events,
			"count":  len(events),
		}); err != nil {
			return
		}
	}
}

// handleHealth returns a simple health check response.
func handleHealth(w http.ResponseWriter, r *http.Request) {
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
