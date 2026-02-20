package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
)

const (
	maxEvents  = 1000
	maxBodyLen = 1 << 20 // 1 MiB — generous limit for hook payloads.
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

// HookEvent represents a single hook event received from Claude Code.
type HookEvent struct {
	HookType  string                 `json:"hook_type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// HookMonitor stores hook events in a bounded ring buffer with thread-safe access.
type HookMonitor struct {
	events []HookEvent
	mu     sync.RWMutex
	stats  map[string]int
}

// NewHookMonitor returns an initialized HookMonitor.
func NewHookMonitor() *HookMonitor {
	return &HookMonitor{
		events: make([]HookEvent, 0, 256),
		stats:  make(map[string]int),
	}
}

// AddEvent appends an event to the buffer (thread-safe, bounded).
// Logging happens outside the lock to avoid blocking readers during I/O.
func (m *HookMonitor) AddEvent(event HookEvent) {
	m.mu.Lock()
	m.events = append(m.events, event)
	// Trim oldest when exceeding max — copy into a fresh slice so the GC
	// can reclaim the old backing array (avoids the slice-pinning leak).
	if len(m.events) > maxEvents {
		keep := len(m.events) - (maxEvents - 100)
		fresh := make([]HookEvent, maxEvents-100, maxEvents+1)
		copy(fresh, m.events[keep:])
		m.events = fresh
	}
	m.stats[event.HookType]++
	m.mu.Unlock()

	logEvent(event)
}

// logEvent prints a colorized event to the console.
// Called outside the lock — builds the full output first, then writes once.
func logEvent(event HookEvent) {
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

	fmt.Print(buf.String())
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
func (m *HookMonitor) GetEvents(limit int) []HookEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	start := len(m.events) - limit
	result := make([]HookEvent, limit)
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

		event := HookEvent{
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

// acquireLock tries to obtain an exclusive flock on a lock file.
// If another instance holds the lock, it reads the existing port file
// and prints info about the running instance, then exits.
func acquireLock(lockPath, portFilePath string) *os.File {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening lock file: %v\n", err)
		os.Exit(1)
	}

	// Try non-blocking exclusive lock.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Lock held by another process — read its info.
		f.Close()
		showRunningInstance(portFilePath)
		os.Exit(1)
	}

	// Write our PID into the lock file for diagnostics.
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "%d", os.Getpid())
	f.Sync()

	return f // caller keeps this open to hold the lock
}

// showRunningInstance reads the port and PID files and prints info about
// the already-running monitor instance.
func showRunningInstance(portFilePath string) {
	warn := color.New(color.FgYellow, color.Bold)
	info := color.New(color.FgCyan)

	warn.Println("\n  Monitor is already running!")
	fmt.Println()

	// Read PID from lock file.
	lockPath := strings.TrimSuffix(portFilePath, ".monitor-port") + ".monitor-lock"
	if pidBytes, err := os.ReadFile(lockPath); err == nil {
		pid := strings.TrimSpace(string(pidBytes))
		info.Printf("  PID:  %s\n", pid)
	}

	// Read port from port file.
	if portBytes, err := os.ReadFile(portFilePath); err == nil {
		port := strings.TrimSpace(string(portBytes))
		info.Printf("  URL:  http://localhost:%s\n", port)

		// Try to fetch stats from the running instance.
		client := &http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Get("http://localhost:" + port + "/stats"); err == nil {
			defer resp.Body.Close()
			var stats map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&stats) == nil {
				if total, ok := stats["total_hooks"]; ok {
					info.Printf("  Hooks received: %v\n", total)
				}
			}
		}
	} else {
		info.Println("  Port: unknown (port file not found)")
	}

	fmt.Println()
	warn.Println("  Use 'kill <PID>' to stop it, or 'make check' to verify status.")
	fmt.Println()
}

func main() {
	// Resolve lock and port file paths.
	portFile := os.Getenv("PORT_FILE")
	if portFile == "" {
		portFile = "hooks/.monitor-port"
	}
	lockFile := portFile[:len(portFile)-len(".monitor-port")] + ".monitor-lock"

	// Single-instance guard.
	lockFd := acquireLock(lockFile, portFile)

	monitor := NewHookMonitor()

	// Banner
	banner := color.New(color.FgHiGreen, color.Bold)
	banner.Println("╔══════════════════════════════════════════════════════════════╗")
	banner.Println("║           Claude Code Hooks Monitor v2.2                    ║")
	banner.Println("╚══════════════════════════════════════════════════════════════╝")

	// All hook types to register.
	hookTypes := []string{
		"SessionStart",
		"UserPromptSubmit",
		"PreToolUse",
		"PermissionRequest",
		"PostToolUse",
		"PostToolUseFailure",
		"Notification",
		"SubagentStart",
		"SubagentStop",
		"Stop",
		"TeammateIdle",
		"TaskCompleted",
		"ConfigChange",
		"PreCompact",
		"SessionEnd",
	}

	for _, ht := range hookTypes {
		http.HandleFunc("/hook/"+ht, handleHook(monitor, ht))
	}

	http.HandleFunc("/stats", handleStats(monitor))
	http.HandleFunc("/events", handleEvents(monitor))
	http.HandleFunc("/health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Try the requested port first, then fall back to OS-assigned port.
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		color.New(color.FgYellow).Printf("  Port %s in use, finding available port...\n", port)
		ln, err = net.Listen("tcp", ":0")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
			os.Exit(1)
		}
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	// Write port file so hook-client can discover us.
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(actualPort)), 0644); err != nil {
		color.New(color.FgYellow).Printf("  Warning: could not write port file %s: %v\n", portFile, err)
	} else {
		fmt.Printf("  Port file: %s\n", portFile)
	}

	// Clean up port file and lock on shutdown signals.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		os.Remove(portFile)
		lockFd.Close()
		os.Remove(lockFile)
		os.Exit(0)
	}()

	fmt.Println()
	color.New(color.FgHiCyan).Printf("  Registered %d hook endpoints\n", len(hookTypes))
	fmt.Println("  Endpoints: /stats  /events  /health")
	fmt.Printf("  Listening on http://localhost:%d\n\n", actualPort)
	color.New(color.FgHiYellow).Println("  Waiting for hook events...")
	fmt.Println()

	if err := http.Serve(ln, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		os.Exit(1)
	}
}
