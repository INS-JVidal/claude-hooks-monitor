package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"claude-hooks-monitor/internal/hookevt"
)

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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"hook":   hookType,
		})
	}
}

// handleStats returns aggregate hook statistics.
func handleStats(monitor *HookMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := monitor.GetStats()
		total := 0
		for _, v := range stats {
			total += v
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"stats":       stats,
			"total_hooks": total,
		})
	}
}

// handleEvents returns the last N events.
func handleEvents(monitor *HookMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				limit = n
			}
		}

		events := monitor.GetEvents(limit)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": events,
			"count":  len(events),
		})
	}
}

// handleHealth returns a simple health check response.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}
