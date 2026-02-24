package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"claude-hooks-monitor/internal/config"
)

const maxStdinLen = 1 << 20 // 1 MiB — generous limit for hook payloads.

// Config holds hook client configuration sourced from environment variables.
type Config struct {
	MonitorURL string
	Timeout    time.Duration
	ConfigPath string
}

// matcherHooks are hook types that require a "matcher": "*" field in their
// settings.json spec. These hooks are tool-scoped and need the wildcard
// matcher to fire for all tool invocations.
var matcherHooks = map[string]bool{
	"PreToolUse":          true,
	"PostToolUse":         true,
	"PostToolUseFailure":  true,
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install-hooks" {
		os.Exit(runInstallHooks())
	}

	// Resolve config path relative to this binary's location.
	// If os.Executable() fails (deleted binary, pipe exec), skip binary-relative search.
	execPath, err := os.Executable()
	hookDir := ""
	if err == nil && execPath != "" {
		hookDir = filepath.Dir(execPath)
	}
	xdgDir := xdgConfigDir()

	timeout := getEnvInt("HOOK_TIMEOUT", 2)
	if timeout > 10 {
		timeout = 10
	}

	projectDir := os.Getenv("CLAUDE_PROJECT_DIR")

	config := Config{
		MonitorURL: discoverMonitorURL(xdgDir, hookDir),
		Timeout:    time.Duration(timeout) * time.Second,
		ConfigPath: discoverFile("HOOK_MONITOR_CONFIG", "hook_monitor.conf", xdgDir, hookDir, projectDir),
	}

	// No monitor URL means we couldn't find a valid target — skip silently.
	if config.MonitorURL == "" {
		os.Exit(0)
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

	// Validate hookType: must be alphanumeric (letters only) to prevent
	// path traversal and ensure config toggle checks match the URL path.
	if !isAlphaOnly(hookType) {
		os.Exit(0)
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

	// Send to monitor server (hookType is already validated as alpha-only above).
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

	targetURL := config.MonitorURL + "/hook/" + hookType
	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Add bearer token if configured.
	if token := os.Getenv("HOOK_MONITOR_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// isHookEnabled reads hook_monitor.conf to check if a hook is enabled.
// Fail-open: missing file, missing key, or any error → true (enabled).
// Key matching is case-insensitive to stay consistent with the Bash skill.
// Inline comments (# ...) are stripped from values before comparison.
//
// Uses os.ReadFile for a single-syscall read, which pairs well with the
// bash skill's atomic temp-file-then-rename write pattern. This also avoids
// the bufio.Scanner 64KiB line-length limit.
func isHookEnabled(configPath, hookName string) bool {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return true // missing or unreadable config = all enabled
	}

	content := string(raw)

	// Strip UTF-8 BOM if present (Windows editors add this).
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")

	inHooksSection := false
	found := false
	foundVal := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)

		// Skip blanks and comments.
		if len(line) == 0 || line[0] == '#' {
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

		// Strip inline comments: "yes # enable this" → "yes"
		if idx := strings.Index(val, "#"); idx >= 0 {
			val = strings.TrimSpace(val[:idx])
		}

		// Case-insensitive key match (e.g. "pretooluse" matches "PreToolUse").
		// Use last-wins semantics for duplicate keys (matches Bash parser behaviour).
		if strings.EqualFold(key, hookName) {
			found = true
			foundVal = val
		}
	}

	if found {
		return !strings.EqualFold(foundVal, "no")
	}
	return true // not found → enabled
}

// xdgConfigDir returns the XDG config directory for the monitor.
// Uses $XDG_CONFIG_HOME if set, otherwise ~/.config/claude-hooks-monitor/.
func xdgConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claude-hooks-monitor")
}

// discoverFile locates a config/runtime file using a priority chain:
//  1. Environment variable override (envKey)
//  2. Project-level: $CLAUDE_PROJECT_DIR/.claude/<filename>
//  3. XDG config dir (~/.config/claude-hooks-monitor/)
//  4. Binary-relative directory (legacy/fallback)
//
// Returns the first path that exists on disk, or the XDG path as default.
// The projectDir parameter enables per-project config overrides; pass "" to skip.
func discoverFile(envKey, filename, xdgDir, hookDir, projectDir string) string {
	// 1. Env var override.
	if envKey != "" {
		if p := os.Getenv(envKey); p != "" {
			return p
		}
	}
	// 2. Project-level override: <projectDir>/.claude/<filename>
	if projectDir != "" {
		projPath := filepath.Join(projectDir, ".claude", filename)
		if _, err := os.Stat(projPath); err == nil {
			return projPath
		}
	}
	// 3. XDG config dir.
	if xdgDir != "" {
		xdgPath := filepath.Join(xdgDir, filename)
		if _, err := os.Stat(xdgPath); err == nil {
			return xdgPath
		}
	}
	// 4. Binary-relative (legacy).
	legacyPath := filepath.Join(hookDir, filename)
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}
	// Default to XDG path (even if it doesn't exist yet — fail-open in isHookEnabled).
	if xdgDir != "" {
		return filepath.Join(xdgDir, filename)
	}
	return legacyPath
}

// discoverMonitorURL returns the monitor URL.
// Priority: HOOK_MONITOR_URL env var → XDG .monitor-port → binary-relative .monitor-port → skip.
func discoverMonitorURL(xdgDir, hookDir string) string {
	if urlStr := os.Getenv("HOOK_MONITOR_URL"); urlStr != "" {
		u, err := url.Parse(urlStr)
		if err != nil {
			return ""
		}
		// Only plain HTTP is supported — the monitor always binds HTTP on loopback.
		if u.Scheme != "http" {
			return ""
		}
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return "" // refuse non-loopback targets
		}
		return urlStr
	}

	// Try port file in priority order: XDG → binary-relative.
	for _, dir := range []string{xdgDir, hookDir} {
		if dir == "" {
			continue
		}
		portFile := filepath.Join(dir, ".monitor-port")
		data, err := os.ReadFile(portFile)
		if err != nil {
			continue
		}
		port := strings.TrimSpace(string(data))
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			continue // invalid port — try next
		}
		return "http://localhost:" + port
	}

	return "" // No monitor URL found — skip sending rather than risk hitting an unrelated service.
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

// isAlphaOnly returns true if s is non-empty and contains only ASCII letters.
// This rejects path traversal attempts ("../../admin") and special characters
// that could cause config check / URL path mismatches.
func isAlphaOnly(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	return true
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

// runInstallHooks registers all hooks in ~/.claude/settings.json.
// It is idempotent: if a "hooks" key already exists, it prints a message and
// exits successfully without modifying the file.
func runInstallHooks() int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine home directory: %v\n", err)
		return 1
	}

	claudeDir := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Ensure ~/.claude/ exists.
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create %s: %v\n", claudeDir, err)
		return 1
	}

	// Read existing settings or start fresh.
	var d map[string]interface{}
	raw, err := os.ReadFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(raw, &d); err != nil {
			fmt.Fprintf(os.Stderr, "invalid JSON in %s: %v\n", settingsPath, err)
			return 1
		}
	} else {
		d = map[string]interface{}{}
	}

	// Idempotent: if "hooks" already exists, do nothing.
	if _, exists := d["hooks"]; exists {
		fmt.Println("Hooks already present in " + settingsPath)
		return 0
	}

	// Build hooks map from config.AllHookTypes.
	h := make(map[string]interface{}, len(config.AllHookTypes))
	hookSpec := []interface{}{
		map[string]interface{}{
			"type":    "command",
			"command": "hook-client",
		},
	}

	for _, name := range config.AllHookTypes {
		entry := map[string]interface{}{
			"hooks": hookSpec,
		}
		if matcherHooks[name] {
			entry["matcher"] = "*"
		}
		h[name] = []interface{}{entry}
	}

	d["hooks"] = h

	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON marshal error: %v\n", err)
		return 1
	}
	out = append(out, '\n')

	// Back up existing settings before writing (recovery path if something goes wrong).
	if raw != nil {
		backupPath := settingsPath + ".bak"
		_ = os.WriteFile(backupPath, raw, 0644)
	}

	if err := config.AtomicWriteFile(settingsPath, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", settingsPath, err)
		return 1
	}

	fmt.Println("Hooks registered in " + settingsPath)
	return 0
}
