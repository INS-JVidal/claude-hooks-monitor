package platform

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

// showRunningInstance reads the port and PID files and prints info about
// the already-running monitor instance.
func ShowRunningInstance(lockPath, portFilePath string) {
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

		// Validate port is numeric and in valid range before using in HTTP request.
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			info.Println("  Port: invalid port file")
			return
		}

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
