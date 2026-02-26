package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// isAlphaOnly — Input Validation (Security-Critical)
// ============================================================

func TestIsAlphaOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Valid
		{"valid_lower", "pretooluse", true},
		{"valid_upper", "PRETOOLUSE", true},
		{"valid_mixed", "PreToolUse", true},
		{"single_char", "a", true},
		{"single_upper", "Z", true},

		// Invalid — empty
		{"empty", "", false},

		// Invalid — numbers
		{"with_numbers", "hook123", false},
		{"only_numbers", "123", false},
		{"leading_number", "1hook", false},

		// Invalid — path traversal
		{"path_traversal", "../../etc/passwd", false},
		{"path_traversal_short", "../admin", false},
		{"double_dot", "..", false},
		{"single_dot", ".", false},

		// Invalid — URL encoding
		{"url_encoded", "%2e%2e", false},
		{"url_slash", "%2f", false},

		// Invalid — injection attempts
		{"slash_inject", "hook/../../admin", false},
		{"space_inject", "hook type", false},
		{"newline_inject", "hook\ntype", false},
		{"null_byte", "hook\x00type", false},
		{"tab", "hook\ttype", false},
		{"carriage_return", "hook\rtype", false},

		// Invalid — unicode
		{"unicode_accent", "hóok", false},
		{"cjk", "日本", false},
		{"emoji", "hook😀", false},

		// Invalid — shell injection
		{"semicolon", "hook;rm -rf /", false},
		{"backtick", "hook`id`", false},
		{"pipe", "hook|cat /etc/passwd", false},
		{"dollar", "hook$(id)", false},
		{"ampersand", "hook&&id", false},

		// Invalid — special chars
		{"hyphen", "pre-tool-use", false},
		{"underscore", "pre_tool_use", false},
		{"at_sign", "hook@type", false},
		{"equals", "hook=type", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isAlphaOnly(tc.in)
			if got != tc.want {
				t.Errorf("isAlphaOnly(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// ============================================================
// truncate — UTF-8 Safety
// ============================================================

func TestTruncate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short_string", "abc", 10, "abc"},
		{"exact_length", "abc", 3, "abc"},
		{"ascii_cut", "abcde", 3, "abc"},
		{"empty_string", "", 5, ""},
		{"zero_maxlen", "abc", 0, ""},

		// Multi-byte: 'é' is 2 bytes (0xC3 0xA9)
		// "café" = c(1) a(1) f(1) é(2) = 5 bytes
		{"multibyte_cafe_4", "café", 4, "caf"},  // can't fit é (starts at byte 3, needs 2 bytes)
		{"multibyte_cafe_5", "café", 5, "café"},  // fits exactly

		// CJK: '日' is 3 bytes each
		// "日本語" = 9 bytes
		{"cjk_4", "日本語", 4, "日"},   // 4 bytes: can fit 日(3), next starts at 3, needs 3 more → only 日
		{"cjk_6", "日本語", 6, "日本"}, // 6 bytes: fits 日(3)+本(3)
		{"cjk_3", "日本語", 3, "日"},   // exactly 3 bytes = 1 char

		// Emoji: '😀' is 4 bytes
		// "hello😀world" = 5 + 4 + 5 = 14 bytes
		{"emoji_boundary_6", "hello😀world", 6, "hello"}, // 6 bytes: can't fit emoji at position 5
		{"emoji_boundary_9", "hello😀world", 9, "hello😀"}, // 9 bytes: fits emoji

		// All ASCII
		{"long_ascii", strings.Repeat("x", 100), 50, strings.Repeat("x", 50)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

// ============================================================
// discoverMonitorURL — URL Validation (Security-Critical)
// ============================================================

func TestDiscoverURL_EnvVar(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string // expected return; empty = rejected
	}{
		// Accepted
		{"localhost", "http://localhost:8080", "http://localhost:8080"},
		{"localhost_no_port", "http://localhost", "http://localhost"},
		{"ipv4", "http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"ipv6", "http://[::1]:8080", "http://[::1]:8080"},

		// Rejected — wrong scheme
		{"https", "https://localhost:8080", ""},
		{"ftp", "ftp://localhost:8080", ""},
		{"no_scheme", "localhost:8080", ""},

		// Rejected — non-loopback hosts
		{"remote_host", "http://evil.com:8080", ""},
		{"ip_remote", "http://10.0.0.1:8080", ""},
		{"metadata_ssrf", "http://169.254.169.254/latest/meta-data", ""},
		{"dns_rebind", "http://attacker.com:8080", ""},
		{"internal_ip", "http://192.168.1.1:8080", ""},

		// Edge cases
		{"empty_url", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.url != "" {
				t.Setenv("HOOK_MONITOR_URL", tc.url)
			} else {
				// For "empty_url" case, make sure env is unset and no port file exists
				t.Setenv("HOOK_MONITOR_URL", "")
			}
			got := discoverMonitorURL("", t.TempDir()) // empty xdgDir, empty hookDir = no port file
			if got != tc.want {
				t.Errorf("discoverMonitorURL with HOOK_MONITOR_URL=%q = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestDiscoverURL_PortFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"valid_8080", "8080", "http://localhost:8080"},
		{"valid_with_newline", "8080\n", "http://localhost:8080"},
		{"valid_with_spaces", "  8080  ", "http://localhost:8080"},
		{"port_1", "1", "http://localhost:1"},
		{"port_65535", "65535", "http://localhost:65535"},

		// Invalid ports
		{"invalid_abc", "abc", ""},
		{"port_zero", "0", ""},
		{"port_negative", "-1", ""},
		{"port_65536", "65536", ""},
		{"port_99999", "99999", ""},
		{"empty_file", "", ""},
		{"float_port", "3.14", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Unset env to fall through to port file
			t.Setenv("HOOK_MONITOR_URL", "")

			dir := t.TempDir()
			portFile := filepath.Join(dir, ".monitor-port")
			os.WriteFile(portFile, []byte(tc.content), 0600)

			got := discoverMonitorURL("", dir)
			if got != tc.want {
				t.Errorf("discoverMonitorURL with port file %q = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestDiscoverURL_PortFileMissing(t *testing.T) {
	t.Setenv("HOOK_MONITOR_URL", "")
	got := discoverMonitorURL("", t.TempDir())
	if got != "" {
		t.Errorf("expected empty URL with missing port file, got %q", got)
	}
}

func TestDiscoverURL_EnvVarPriority(t *testing.T) {
	// Env var should take priority over port file
	t.Setenv("HOOK_MONITOR_URL", "http://localhost:9999")

	dir := t.TempDir()
	portFile := filepath.Join(dir, ".monitor-port")
	os.WriteFile(portFile, []byte("8080"), 0600)

	got := discoverMonitorURL("", dir)
	if got != "http://localhost:9999" {
		t.Errorf("expected env var priority, got %q", got)
	}
}

// ============================================================
// isHookEnabled — Config Parsing
// ============================================================

func TestIsHookEnabled(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		hookName string
		want     bool
	}{
		{"disabled", "[hooks]\nPreToolUse = no\n", "PreToolUse", false},
		{"enabled", "[hooks]\nPreToolUse = yes\n", "PreToolUse", true},
		{"bom", "\xef\xbb\xbf[hooks]\nPreToolUse = no\n", "PreToolUse", false},
		{"case_insensitive_key", "[hooks]\npretooluse = no\n", "PreToolUse", false},
		{"case_insensitive_value", "[hooks]\nPreToolUse = NO\n", "PreToolUse", false},
		{"inline_comment", "[hooks]\nPreToolUse = no # disabled\n", "PreToolUse", false},
		{"wrong_section", "[other]\nPreToolUse = no\n", "PreToolUse", true},
		{"last_wins_no", "[hooks]\nPreToolUse = yes\nPreToolUse = no\n", "PreToolUse", false},
		{"last_wins_yes", "[hooks]\nPreToolUse = no\nPreToolUse = yes\n", "PreToolUse", true},
		{"empty_value", "[hooks]\nPreToolUse = \n", "PreToolUse", true}, // empty != "no"
		{"missing_key", "[hooks]\nSessionStart = yes\n", "PreToolUse", true},
		{"empty_file", "", "PreToolUse", true},
		{"only_comments", "# comment\n# another\n", "PreToolUse", true},
		{"no_equals", "[hooks]\nPreToolUse\n", "PreToolUse", true}, // skipped
		{"windows_line_endings", "[hooks]\r\nPreToolUse = no\r\n", "PreToolUse", false},
		{"extra_spaces", "[hooks]\n  PreToolUse  =  no  \n", "PreToolUse", false},
		{"value_with_hash_in_it", "[hooks]\nPreToolUse = no#yes\n", "PreToolUse", false}, // strips inline comment → "no"
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.conf")
			os.WriteFile(path, []byte(tc.content), 0600)

			got := isHookEnabled(path, tc.hookName)
			if got != tc.want {
				t.Errorf("isHookEnabled(%q, %q) with content %q = %v, want %v",
					path, tc.hookName, tc.content, got, tc.want)
			}
		})
	}
}

func TestIsHookEnabled_MissingFile(t *testing.T) {
	// Fail-open: missing file → enabled
	got := isHookEnabled("/nonexistent/config.conf", "PreToolUse")
	if !got {
		t.Error("missing config file should default to enabled (fail-open)")
	}
}

func TestIsHookEnabled_MultipleSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	// [hooks] sets no, then [other], then [hooks] sets yes → last-wins
	content := "[hooks]\nPreToolUse = no\n[other]\nFoo = bar\n[hooks]\nPreToolUse = yes\n"
	os.WriteFile(path, []byte(content), 0600)

	got := isHookEnabled(path, "PreToolUse")
	if !got {
		t.Error("expected second [hooks] section to override with yes")
	}
}

// ============================================================
// getEnv
// ============================================================

func TestGetEnv(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_KEY", "myvalue")
		if got := getEnv("TEST_GET_ENV_KEY", "default"); got != "myvalue" {
			t.Errorf("got %q, want %q", got, "myvalue")
		}
	})

	t.Run("unset", func(t *testing.T) {
		if got := getEnv("TEST_GET_ENV_UNSET_KEY_12345", "default"); got != "default" {
			t.Errorf("got %q, want %q", got, "default")
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_EMPTY", "")
		if got := getEnv("TEST_GET_ENV_EMPTY", "default"); got != "default" {
			t.Errorf("got %q, want %q (empty treated as unset)", got, "default")
		}
	})
}

// ============================================================
// getEnvInt
// ============================================================

func TestGetEnvInt(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		set      bool
		defVal   int
		expected int
	}{
		{"valid", "5", true, 2, 5},
		{"unset", "", false, 2, 2},
		{"empty_string", "", true, 2, 2},
		{"non_numeric", "abc", true, 2, 2},
		{"zero", "0", true, 2, 2},       // n <= 0 returns default
		{"negative", "-1", true, 2, 2},   // n <= 0 returns default
		{"large", "99999", true, 2, 99999},
		{"float", "3.14", true, 2, 2},   // Atoi fails
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := fmt.Sprintf("TEST_GETENVINT_%s", tc.name)
			if tc.set {
				t.Setenv(key, tc.value)
			}
			got := getEnvInt(key, tc.defVal)
			if got != tc.expected {
				t.Errorf("getEnvInt(%q, %d) = %d, want %d", tc.value, tc.defVal, got, tc.expected)
			}
		})
	}
}

// ============================================================
// hasClaudeMD — CLAUDE.md detection
// ============================================================

func TestHasClaudeMD_Exists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# test"), 0644)

	data := map[string]interface{}{"cwd": dir}
	if !hasClaudeMD(data) {
		t.Error("hasClaudeMD should return true when CLAUDE.md exists")
	}
}

func TestHasClaudeMD_NotExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	data := map[string]interface{}{"cwd": dir}
	if hasClaudeMD(data) {
		t.Error("hasClaudeMD should return false when CLAUDE.md does not exist")
	}
}

func TestHasClaudeMD_NoCwd(t *testing.T) {
	t.Parallel()

	data := map[string]interface{}{}
	if hasClaudeMD(data) {
		t.Error("hasClaudeMD should return false when cwd is missing")
	}
}

func TestHasClaudeMD_EmptyCwd(t *testing.T) {
	t.Parallel()

	data := map[string]interface{}{"cwd": ""}
	if hasClaudeMD(data) {
		t.Error("hasClaudeMD should return false when cwd is empty")
	}
}

func TestHasClaudeMD_NonStringCwd(t *testing.T) {
	t.Parallel()

	data := map[string]interface{}{"cwd": 42}
	if hasClaudeMD(data) {
		t.Error("hasClaudeMD should return false when cwd is not a string")
	}
}

// ============================================================
// sendToMonitor — integration with real httptest server
// ============================================================

func TestSendToMonitor_HappyPath(t *testing.T) {
	var receivedPath string
	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		fmt.Fprintf(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv("HOOK_MONITOR_TOKEN", "test-token")

	config := Config{
		MonitorURL: srv.URL,
		Timeout:    2 * 1e9, // 2 seconds
	}
	payload, _ := json.Marshal(map[string]string{"key": "value"})
	sendToMonitor(config, "PreToolUse", payload)

	if receivedPath != "/hook/PreToolUse" {
		t.Errorf("path = %q, want /hook/PreToolUse", receivedPath)
	}
	if receivedAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want 'Bearer test-token'", receivedAuth)
	}
}

func TestSendToMonitor_NoToken(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	t.Setenv("HOOK_MONITOR_TOKEN", "")

	config := Config{
		MonitorURL: srv.URL,
		Timeout:    2 * 1e9,
	}
	sendToMonitor(config, "PreToolUse", []byte(`{}`))

	if receivedAuth != "" {
		t.Errorf("auth should be empty when no token, got %q", receivedAuth)
	}
}

func TestSendToMonitor_ServerDown(t *testing.T) {
	// Should not panic when server is unreachable
	config := Config{
		MonitorURL: "http://127.0.0.1:1", // port 1 is unlikely to be listening
		Timeout:    100 * 1e6,             // 100ms — short timeout for test
	}
	sendToMonitor(config, "PreToolUse", []byte(`{}`))
	// If we get here without panic, test passes
}

func TestSendToMonitor_InvalidURL(t *testing.T) {
	// Should not panic with malformed URL
	config := Config{
		MonitorURL: "://invalid-url",
		Timeout:    100 * 1e6,
	}
	sendToMonitor(config, "PreToolUse", []byte(`{}`))
	// If we get here without panic, test passes
}

// ============================================================
// enrichPostToolUseCache — PostToolUse Read file stat enrichment
// ============================================================

func TestEnrichPostToolUseCache_ReadTool(t *testing.T) {
	t.Parallel()

	// Create a temp file to stat.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.go")
	os.WriteFile(filePath, []byte("package main"), 0644)

	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": filePath},
	}
	enrichPostToolUseCache(inputData)

	cache, ok := inputData["_cache"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _cache metadata to be added")
	}
	if cache["file_path"] != filepath.Clean(filePath) {
		t.Errorf("file_path = %v, want %s", cache["file_path"], filepath.Clean(filePath))
	}
	if _, ok := cache["mtime_ns"]; !ok {
		t.Error("expected mtime_ns in _cache")
	}
	if size, ok := cache["size"].(int64); !ok || size != 12 {
		t.Errorf("size = %v, want 12", cache["size"])
	}
}

func TestEnrichPostToolUseCache_NonReadTool(t *testing.T) {
	t.Parallel()
	inputData := map[string]interface{}{
		"tool_name":  "Write",
		"tool_input": map[string]interface{}{"file_path": "/some/file.go"},
	}
	enrichPostToolUseCache(inputData)

	if _, ok := inputData["_cache"]; ok {
		t.Error("_cache should not be added for non-Read tools")
	}
}

func TestEnrichPostToolUseCache_NoToolInput(t *testing.T) {
	t.Parallel()
	inputData := map[string]interface{}{
		"tool_name": "Read",
	}
	enrichPostToolUseCache(inputData)

	if _, ok := inputData["_cache"]; ok {
		t.Error("_cache should not be added without tool_input")
	}
}

func TestEnrichPostToolUseCache_NonexistentFile(t *testing.T) {
	t.Parallel()
	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "/nonexistent/file.go"},
	}
	enrichPostToolUseCache(inputData)

	if _, ok := inputData["_cache"]; ok {
		t.Error("_cache should not be added for nonexistent files")
	}
}

// ============================================================
// handlePreToolUseRead — PreToolUse Read interception
// ============================================================

func TestHandlePreToolUseRead_Unchanged(t *testing.T) {
	// Create a temp file.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "handler.go")
	os.WriteFile(filePath, []byte("package main"), 0644)

	info, _ := os.Stat(filePath)
	mtimeNS := info.ModTime().UnixNano()
	size := info.Size()

	// Serve a fake cache response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found":        true,
			"file_path":    filePath,
			"mtime_ns":     mtimeNS,
			"size":         size,
			"reads_ago":    3,
			"last_read_at": time.Now().Add(-5 * time.Minute),
		})
	}))
	defer srv.Close()

	cfg := Config{MonitorURL: srv.URL, Timeout: 2 * time.Second}
	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": filePath},
		"session_id": "test-session",
	}

	output := handlePreToolUseRead(cfg, inputData)
	if output == nil {
		t.Fatal("expected annotation output for unchanged file")
	}
	if output.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("permissionDecision = %q, want allow", output.HookSpecificOutput.PermissionDecision)
	}
	if !strings.Contains(output.HookSpecificOutput.AdditionalContext, "handler.go") {
		t.Errorf("annotation should mention filename, got: %s", output.HookSpecificOutput.AdditionalContext)
	}
	if !strings.Contains(output.HookSpecificOutput.AdditionalContext, "3 reads ago") {
		t.Errorf("annotation should mention reads ago, got: %s", output.HookSpecificOutput.AdditionalContext)
	}
}

func TestHandlePreToolUseRead_Changed(t *testing.T) {
	// Create a temp file.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "handler.go")
	os.WriteFile(filePath, []byte("package main"), 0644)

	// Serve a cache response with DIFFERENT mtime (simulating file was modified).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found":     true,
			"file_path": filePath,
			"mtime_ns":  int64(999), // doesn't match actual file
			"size":      int64(12),
			"reads_ago": 1,
		})
	}))
	defer srv.Close()

	cfg := Config{MonitorURL: srv.URL, Timeout: 2 * time.Second}
	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": filePath},
		"session_id": "test-session",
	}

	output := handlePreToolUseRead(cfg, inputData)
	if output != nil {
		t.Error("expected nil output for changed file")
	}
}

func TestHandlePreToolUseRead_NotInCache(t *testing.T) {
	// Serve a "not found" cache response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"found": false})
	}))
	defer srv.Close()

	cfg := Config{MonitorURL: srv.URL, Timeout: 2 * time.Second}
	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "/some/new/file.go"},
		"session_id": "test-session",
	}

	output := handlePreToolUseRead(cfg, inputData)
	if output != nil {
		t.Error("expected nil output for file not in cache")
	}
}

func TestHandlePreToolUseRead_NonReadTool(t *testing.T) {
	cfg := Config{MonitorURL: "http://localhost:1", Timeout: 100 * time.Millisecond}
	inputData := map[string]interface{}{
		"tool_name":  "Write",
		"tool_input": map[string]interface{}{"file_path": "/some/file.go"},
		"session_id": "test-session",
	}

	output := handlePreToolUseRead(cfg, inputData)
	if output != nil {
		t.Error("expected nil output for non-Read tool")
	}
}

func TestHandlePreToolUseRead_NoSessionID(t *testing.T) {
	cfg := Config{MonitorURL: "http://localhost:1", Timeout: 100 * time.Millisecond}
	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "/some/file.go"},
	}

	output := handlePreToolUseRead(cfg, inputData)
	if output != nil {
		t.Error("expected nil output when session_id is missing")
	}
}

func TestHandlePreToolUseRead_MonitorDown(t *testing.T) {
	cfg := Config{MonitorURL: "http://127.0.0.1:1", Timeout: 100 * time.Millisecond}
	inputData := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "/some/file.go"},
		"session_id": "test-session",
	}

	output := handlePreToolUseRead(cfg, inputData)
	if output != nil {
		t.Error("expected nil output when monitor is unreachable")
	}
}
