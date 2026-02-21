package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

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
	lockFile := portFile[:len(portFile)-len(".monitor-port")] + ".monitor-lock"

	// Single-instance guard.
	lockFd := acquireLock(lockFile, portFile)

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
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		if !*uiMode {
			color.New(color.FgYellow).Printf("  Port %s in use, finding available port...\n", port)
		}
		ln, err = net.Listen("tcp", ":0")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
			os.Exit(1)
		}
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	// Write port file so hook-client can discover us.
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(actualPort)), 0644); err != nil {
		if !*uiMode {
			color.New(color.FgYellow).Printf("  Warning: could not write port file %s: %v\n", portFile, err)
		}
	} else if !*uiMode {
		fmt.Printf("  Port file: %s\n", portFile)
	}

	// Coordinated shutdown context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &http.Server{Handler: mux}

	if *uiMode {
		// Start HTTP server in background.
		go server.Serve(ln)

		// Cleanup on exit (deferred, runs after TUI quits).
		defer func() {
			server.Shutdown(context.Background())
			os.Remove(portFile)
			lockFd.Close()
			os.Remove(lockFile)
		}()

		// Run TUI (blocks until user quits).
		if err := tui.Run(ctx, eventCh, actualPort); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
	} else {
		// Existing behavior: signal handler + blocking serve.
		printBanner(actualPort, len(hookTypes))
		setupSignalHandler(portFile, lockFile, lockFd)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
			os.Exit(1)
		}
	}
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

// setupSignalHandler registers cleanup for SIGINT/SIGTERM in console mode.
func setupSignalHandler(portFile, lockFile string, lockFd *os.File) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		os.Remove(portFile)
		lockFd.Close()
		os.Remove(lockFile)
		os.Exit(0)
	}()
}
