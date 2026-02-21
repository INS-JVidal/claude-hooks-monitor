package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
)

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
		showRunningInstance(lockPath, portFilePath)
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
func showRunningInstance(lockPath, portFilePath string) {
	warn := color.New(color.FgYellow, color.Bold)
	info := color.New(color.FgCyan)

	warn.Println("\n  Monitor is already running!")
	fmt.Println()

	// Read PID from lock file.
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
