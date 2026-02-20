package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxStdinLen = 1 << 20 // 1 MiB — generous limit for hook payloads.

// Config holds hook client configuration sourced from environment variables.
type Config struct {
	MonitorURL string
	Timeout    time.Duration
	ConfigPath string
}

func main() {
	// Resolve config path relative to this binary's location.
	execPath, _ := os.Executable()
	hookDir := filepath.Dir(execPath)

	config := Config{
		MonitorURL: discoverMonitorURL(hookDir),
		Timeout:    time.Duration(getEnvInt("HOOK_TIMEOUT", 5)) * time.Second,
		ConfigPath: filepath.Join(hookDir, "hook_monitor.conf"),
	}

	// Read JSON from stdin (bounded to prevent runaway memory usage).
	stdinData, err := io.ReadAll(io.LimitReader(os.Stdin, maxStdinLen))
	if err != nil {
		os.Exit(0)
	}

	// Parse input JSON.
	var inputData map[string]interface{}
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &inputData); err != nil {
			inputData = map[string]interface{}{
				"raw_input": truncate(string(stdinData), 2000),
				"error":     "invalid JSON input",
			}
		}
	} else {
		inputData = map[string]interface{}{}
	}

	// Extract hook type from the JSON payload (Claude Code sets this automatically).
	hookType, _ := inputData["hook_event_name"].(string)
	if hookType == "" {
		hookType = getEnv("HOOK_TYPE", "Unknown")
	}

	// Check toggle config — skip if this hook is disabled.
	if !isHookEnabled(config.ConfigPath, hookType) {
		os.Exit(0)
	}

	// Enrich with monitor metadata.
	inputData["_monitor"] = map[string]interface{}{
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"project_dir": inputData["cwd"],
		"plugin_root": os.Getenv("CLAUDE_PLUGIN_ROOT"),
		"is_remote":   os.Getenv("CLAUDE_CODE_REMOTE") == "true",
	}

	// Marshal to JSON.
	payload, err := json.Marshal(inputData)
	if err != nil {
		os.Exit(0)
	}

	// Send to monitor server.
	sendToMonitor(config, hookType, payload)

	// Always exit 0 — never block Claude.
	os.Exit(0)
}

// sendToMonitor POSTs the payload to the monitor server's hook endpoint.
func sendToMonitor(config Config, hookType string, payload []byte) {
	client := &http.Client{
		Timeout:   config.Timeout,
		Transport: &http.Transport{DisableKeepAlives: true},
	}

	url := config.MonitorURL + "/hook/" + hookType
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// isHookEnabled reads hook_monitor.conf to check if a hook is enabled.
// Fail-open: missing file, missing key, or any error → true (enabled).
func isHookEnabled(configPath, hookName string) bool {
	f, err := os.Open(configPath)
	if err != nil {
		return true // missing config = all enabled
	}
	defer f.Close()

	inHooksSection := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and blanks.
		if line == "" || line[0] == '#' {
			continue
		}

		// Section header.
		if line[0] == '[' {
			inHooksSection = strings.EqualFold(line, "[hooks]")
			continue
		}

		if !inHooksSection {
			continue
		}

		// Parse "Key = value".
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if key == hookName {
			return !strings.EqualFold(val, "no")
		}
	}

	return true // not found → enabled
}

// discoverMonitorURL returns the monitor URL.
// Priority: HOOK_MONITOR_URL env var → .monitor-port file → default.
func discoverMonitorURL(hookDir string) string {
	if url := os.Getenv("HOOK_MONITOR_URL"); url != "" {
		return url
	}

	portFile := filepath.Join(hookDir, ".monitor-port")
	data, err := os.ReadFile(portFile)
	if err == nil {
		port := strings.TrimSpace(string(data))
		if port != "" {
			return "http://localhost:" + port
		}
	}

	return "http://localhost:8080"
}

// truncate limits a string to approximately maxLen bytes without
// splitting multi-byte UTF-8 characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Walk backwards from the cut point to find a valid rune boundary.
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}

// getEnv returns the environment variable value or a default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt parses an integer from an environment variable or returns a default.
func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return defaultValue
	}
	return n
}
