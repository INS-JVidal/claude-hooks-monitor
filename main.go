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
	"syscall"
	"time"

	"claude-hooks-monitor/internal/hookevt"
	"claude-hooks-monitor/tui"

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
	lockFd := acquireLock(lockFile, portFile)

	// Remove stale port file from a previous crash. Lock acquisition proves
	// we're the only instance, so any existing port file is stale.
	os.Remove(portFile)

	// Create event channel for TUI mode.
	var eventCh chan hookevt.HookEvent
	if *uiMode {
		eventCh = make(chan hookevt.HookEvent, 256)
	}
	monitor := NewHookMonitor(eventCh)

	// Register HTTP handlers on a dedicated mux (avoids polluting DefaultServeMux).
	mux := http.NewServeMux()
	for _, ht := range hookTypes {
		mux.HandleFunc("/hook/"+ht, handleHook(monitor, ht))
	}
	mux.HandleFunc("/stats", handleStats(monitor))
	mux.HandleFunc("/events", handleEvents(monitor))
	mux.HandleFunc("/health", handleHealth)

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

	// Write port file so hook-client can discover us.
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(actualPort)), 0600); err != nil {
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
	handler = securityHeaders(handler)
	token := os.Getenv("HOOK_MONITOR_TOKEN")
	if token != "" {
		handler = authMiddleware(token, handler)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Unified cleanup — deferred so it runs on both normal exit and signal-triggered exit.
	// No os.Exit calls below this point; both modes fall through to defers.
	defer func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
		os.Remove(portFile)
		lockFd.Close()
		os.Remove(lockFile)
	}()

	// Unified signal handler for both modes.
	// Cancels context (signals TUI to quit) and shuts down HTTP server
	// (unblocks server.Serve in console mode).
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
		sigCtx, sigCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sigCancel()
		server.Shutdown(sigCtx)
	}()

	if *uiMode {
		go server.Serve(ln)

		// Run TUI (blocks until user quits or ctx is cancelled).
		if err := tui.Run(ctx, eventCh, actualPort, &monitor.Dropped); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
	} else {
		printBanner(actualPort, len(hookTypes))
		// Blocks until server.Shutdown is called (from signal handler).
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
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
