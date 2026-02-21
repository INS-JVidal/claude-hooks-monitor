package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-hooks-monitor/internal/hookevt"
	"claude-hooks-monitor/internal/monitor"
	"claude-hooks-monitor/internal/platform"
	"claude-hooks-monitor/internal/server"
	"claude-hooks-monitor/internal/tui"

	"github.com/fatih/color"
)

// hookTypes lists all hook types to register as HTTP endpoints.
var hookTypes = []string{
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

func main() {
	uiMode := flag.Bool("ui", false, "Start interactive tree UI")
	flag.Parse()

	// Resolve lock and port file paths.
	portFile := os.Getenv("PORT_FILE")
	if portFile == "" {
		portFile = "hooks/.monitor-port"
	}
	// Reject absolute paths or path traversal in PORT_FILE.
	if filepath.IsAbs(portFile) || strings.Contains(portFile, "..") {
		fmt.Fprintf(os.Stderr, "Error: PORT_FILE must be a relative path without '..'\n")
		os.Exit(1)
	}
	lockFile := strings.TrimSuffix(portFile, ".monitor-port") + ".monitor-lock"

	// Single-instance guard.
	lockFd := platform.AcquireLock(lockFile, portFile)

	// Remove stale port file from a previous crash. Lock acquisition proves
	// we're the only instance, so any existing port file is stale.
	os.Remove(portFile)

	// Create event channel for TUI mode.
	var eventCh chan hookevt.HookEvent
	if *uiMode {
		eventCh = make(chan hookevt.HookEvent, 256)
	}
	mon := monitor.NewHookMonitor(eventCh)

	// Register HTTP handlers on a dedicated mux (avoids polluting DefaultServeMux).
	mux := http.NewServeMux()
	for _, ht := range hookTypes {
		mux.HandleFunc("/hook/"+ht, server.HandleHook(mon, ht))
	}
	// Catch-all for unknown hook types — returns 404 with an informative message
	// instead of the default mux's generic "404 page not found".
	mux.HandleFunc("/hook/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"unknown hook type"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/stats", server.HandleStats(mon))
	mux.HandleFunc("/events", server.HandleEvents(mon))
	mux.HandleFunc("/health", server.HandleHealth)

	// Listen on requested port, fall back to OS-assigned.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		if !*uiMode {
			color.New(color.FgYellow).Printf("  Port %s in use, finding available port...\n", port)
		}
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
			os.Exit(1)
		}
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	// Write port file atomically (temp + rename) so hook-client never reads
	// a partial or empty file during the brief write window.
	if err := atomicWriteFile(portFile, []byte(strconv.Itoa(actualPort)), 0600); err != nil {
		if !*uiMode {
			color.New(color.FgYellow).Printf("  Warning: could not write port file %s: %v\n", portFile, err)
		}
	} else if !*uiMode {
		fmt.Printf("  Port file: %s\n", portFile)
	}

	// Coordinated shutdown: context signals goroutines, deferred cleanup always runs.
	ctx, cancel := context.WithCancel(context.Background())

	// Wrap mux with security headers; optionally add bearer token auth.
	var handler http.Handler = mux
	handler = server.SecurityHeaders(handler)
	token := os.Getenv("HOOK_MONITOR_TOKEN")
	if token != "" {
		handler = server.AuthMiddleware(token, handler)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Consolidated shutdown — cancel context + gracefully drain HTTP server.
	// sync.Once ensures this runs exactly once regardless of trigger (signal vs normal exit).
	var shutdownOnce sync.Once
	doShutdown := func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}

	// Deferred cleanup — always runs when main() returns.
	defer func() {
		shutdownOnce.Do(doShutdown)
		// Close the TUI event channel via CloseChannel() — this atomically sets
		// a "closed" flag under the monitor's lock, preventing any in-flight
		// AddEvent from sending on the closed channel (which would panic).
		mon.CloseChannel()
		os.Remove(portFile)
		lockFd.Close()
		os.Remove(lockFile)
	}()

	// Signal handler — cancels context and shuts down server on SIGINT/SIGTERM.
	// Selects on ctx.Done() so it exits cleanly on normal shutdown (no goroutine leak).
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, platform.ShutdownSignals...)
		defer signal.Stop(sig)
		select {
		case <-sig:
			shutdownOnce.Do(doShutdown)
		case <-ctx.Done():
			// Normal exit (TUI quit or server stopped) — nothing to do.
		}
	}()

	if *uiMode {
		go srv.Serve(ln)

		// Run TUI (blocks until user quits or ctx is cancelled).
		if err := tui.Run(ctx, eventCh, actualPort, &mon.Dropped); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
	} else {
		printBanner(actualPort, len(hookTypes))
		// Blocks until server.Shutdown is called (from signal handler).
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		}
	}
	// Both paths fall through here → deferred cleanup runs.
}

// printBanner displays the startup banner in console mode.
func printBanner(port, numHooks int) {
	banner := color.New(color.FgHiGreen, color.Bold)
	banner.Println("╔══════════════════════════════════════════════════════════════╗")
	banner.Println("║           Claude Code Hooks Monitor v2.2                    ║")
	banner.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	color.New(color.FgHiCyan).Printf("  Registered %d hook endpoints\n", numHooks)
	fmt.Println("  Endpoints: /stats  /events  /health")
	fmt.Printf("  Listening on http://localhost:%d\n\n", port)
	color.New(color.FgHiYellow).Println("  Waiting for hook events...")
	fmt.Println()
}

// atomicWriteFile writes data to a temp file and renames it to the target path.
// Readers never see a partial file — they get either the old content or the new.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// Rename is atomic on the same filesystem (POSIX guarantee).
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
