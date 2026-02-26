package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadConfig_MissingFile(t *testing.T) {
	cfg, err := ReadConfig("/nonexistent/path/config.conf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	// All hooks should default to enabled (fail-open).
	if len(cfg.Hooks) != len(AllHookTypes) {
		t.Fatalf("expected %d hooks, got %d", len(AllHookTypes), len(cfg.Hooks))
	}
	for _, h := range cfg.Hooks {
		if !h.Enabled {
			t.Errorf("hook %s should default to enabled", h.Name)
		}
	}
}

func TestReadConfig_AllEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nSessionStart = yes\nPreToolUse = yes\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range cfg.Hooks {
		if !h.Enabled {
			t.Errorf("hook %s should be enabled", h.Name)
		}
	}
}

func TestReadConfig_MixedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nSessionStart = yes\nPreToolUse = no\nPostToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	lookup := make(map[string]bool)
	for _, h := range cfg.Hooks {
		lookup[h.Name] = h.Enabled
	}

	if !lookup["SessionStart"] {
		t.Error("SessionStart should be enabled")
	}
	if lookup["PreToolUse"] {
		t.Error("PreToolUse should be disabled")
	}
	if lookup["PostToolUse"] {
		t.Error("PostToolUse should be disabled")
	}
	// Missing key defaults to enabled.
	if !lookup["Notification"] {
		t.Error("Notification (missing key) should default to enabled")
	}
}

func TestReadConfig_BOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "\xef\xbb\xbf[hooks]\nPreToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range cfg.Hooks {
		if h.Name == "PreToolUse" && h.Enabled {
			t.Error("PreToolUse should be disabled (BOM file)")
		}
	}
}

func TestReadConfig_InlineComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nPreToolUse = no # disable this\nPostToolUse = yes # keep on\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	lookup := make(map[string]bool)
	for _, h := range cfg.Hooks {
		lookup[h.Name] = h.Enabled
	}

	if lookup["PreToolUse"] {
		t.Error("PreToolUse should be disabled (inline comment)")
	}
	if !lookup["PostToolUse"] {
		t.Error("PostToolUse should be enabled (inline comment)")
	}
}

func TestReadConfig_CaseInsensitiveKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\npretooluse = no\nPOSTTOOLUSE = no\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	lookup := make(map[string]bool)
	for _, h := range cfg.Hooks {
		lookup[h.Name] = h.Enabled
	}

	if lookup["PreToolUse"] {
		t.Error("PreToolUse should be disabled (case-insensitive key)")
	}
	if lookup["PostToolUse"] {
		t.Error("PostToolUse should be disabled (case-insensitive key)")
	}
}

func TestReadConfig_LastWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nPreToolUse = yes\nPreToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range cfg.Hooks {
		if h.Name == "PreToolUse" && h.Enabled {
			t.Error("PreToolUse should be disabled (last-wins)")
		}
	}
}

func TestWriteConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")

	// Write with some disabled.
	original := HookConfig{Hooks: make([]HookEntry, len(AllHookTypes))}
	for i, name := range AllHookTypes {
		original.Hooks[i] = HookEntry{Name: name, Enabled: name != "PreToolUse" && name != "Stop"}
	}

	if err := WriteConfig(path, original); err != nil {
		t.Fatal(err)
	}

	// Read back.
	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	for i, h := range cfg.Hooks {
		if h.Name != original.Hooks[i].Name {
			t.Errorf("hook %d: name mismatch: got %s, want %s", i, h.Name, original.Hooks[i].Name)
		}
		if h.Enabled != original.Hooks[i].Enabled {
			t.Errorf("hook %s: enabled mismatch: got %v, want %v", h.Name, h.Enabled, original.Hooks[i].Enabled)
		}
	}
}

func TestWriteConfig_ContainsHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")

	cfg := HookConfig{Hooks: []HookEntry{{Name: "SessionStart", Enabled: true}}}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "[hooks]") {
		t.Error("written file should contain [hooks] section header")
	}
	if !strings.Contains(content, "Changes take effect immediately") {
		t.Error("written file should contain header comments")
	}
}

func TestIsEnabled_MatchesHookClient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nPreToolUse = no\nSessionStart = yes\n"
	os.WriteFile(path, []byte(content), 0600)

	if IsEnabled(path, "PreToolUse") {
		t.Error("PreToolUse should be disabled")
	}
	if !IsEnabled(path, "SessionStart") {
		t.Error("SessionStart should be enabled")
	}
	// Missing key defaults to enabled.
	if !IsEnabled(path, "Notification") {
		t.Error("Notification (missing) should default to enabled")
	}
	// Missing file defaults to enabled.
	if !IsEnabled("/nonexistent", "PreToolUse") {
		t.Error("missing file should default to enabled")
	}
}

func TestIsEnabled_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\npretooluse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	if IsEnabled(path, "PreToolUse") {
		t.Error("PreToolUse should be disabled (case-insensitive)")
	}
	if IsEnabled(path, "PRETOOLUSE") {
		t.Error("PRETOOLUSE should be disabled (case-insensitive)")
	}
}

// ============================================================
// ReadSinkConfig
// ============================================================

func TestReadSinkConfig_Defaults(t *testing.T) {
	cfg := ReadSinkConfig("/nonexistent/path")
	if cfg.Forward {
		t.Error("Forward should default to false")
	}
	if cfg.Endpoint != "http://localhost:9800/ingest" {
		t.Errorf("Endpoint = %q, want default", cfg.Endpoint)
	}
}

func TestReadSinkConfig_Enabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[sink]\nforward = yes\nendpoint = http://example.com/events\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg := ReadSinkConfig(path)
	if !cfg.Forward {
		t.Error("Forward should be true")
	}
	if cfg.Endpoint != "http://example.com/events" {
		t.Errorf("Endpoint = %q, want http://example.com/events", cfg.Endpoint)
	}
}

func TestReadSinkConfig_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[SINK]\nForward = YES\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg := ReadSinkConfig(path)
	if !cfg.Forward {
		t.Error("Forward should be true (case-insensitive)")
	}
}

func TestReadSinkConfig_InlineComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[sink]\nforward = yes # enable forwarding\nendpoint = http://localhost:9800/ingest # default\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg := ReadSinkConfig(path)
	if !cfg.Forward {
		t.Error("Forward should be true (inline comment stripped)")
	}
}

func TestReadSinkConfig_NoForwardKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[sink]\nendpoint = http://custom:8080/ingest\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg := ReadSinkConfig(path)
	if cfg.Forward {
		t.Error("Forward should default to false when key missing")
	}
	if cfg.Endpoint != "http://custom:8080/ingest" {
		t.Errorf("Endpoint = %q, want http://custom:8080/ingest", cfg.Endpoint)
	}
}

// ============================================================
// WriteConfig preserves non-[hooks] sections
// ============================================================

func TestWriteConfig_PreservesSinkSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")

	// Write initial file with both sections.
	initial := "[hooks]\nSessionStart = yes\n\n[sink]\nforward = yes\nendpoint = http://localhost:9800/ingest\n"
	os.WriteFile(path, []byte(initial), 0600)

	// Toggle a hook via WriteConfig.
	cfg := HookConfig{Hooks: []HookEntry{
		{Name: "SessionStart", Enabled: false},
	}}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Verify [sink] section survived.
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "[sink]") {
		t.Error("WriteConfig destroyed [sink] section")
	}
	if !strings.Contains(content, "forward = yes") {
		t.Error("WriteConfig destroyed sink forward setting")
	}
	if !strings.Contains(content, "endpoint = http://localhost:9800/ingest") {
		t.Error("WriteConfig destroyed sink endpoint setting")
	}

	// Verify hook was actually toggled.
	if !strings.Contains(content, "SessionStart = no") {
		t.Error("WriteConfig did not toggle SessionStart to no")
	}
}

func TestWriteConfig_NoExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new_config.conf")

	cfg := HookConfig{Hooks: []HookEntry{
		{Name: "SessionStart", Enabled: true},
	}}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "[hooks]") {
		t.Error("WriteConfig should write [hooks] section")
	}
	if !strings.Contains(content, "SessionStart = yes") {
		t.Error("WriteConfig should write hook entry")
	}
}

// ============================================================
// ReadCacheConfig
// ============================================================

func TestReadCacheConfig_DefaultEnabled(t *testing.T) {
	// Missing file should default to enabled (fail-open)
	cfg := ReadCacheConfig("/nonexistent/config.conf")
	if !cfg.Enabled {
		t.Error("missing file should default to enabled")
	}
}

func TestReadCacheConfig_Disabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	os.WriteFile(path, []byte("[cache]\nenabled = no\n"), 0600)

	cfg := ReadCacheConfig(path)
	if cfg.Enabled {
		t.Error("expected enabled=false when set to no")
	}
}

func TestReadCacheConfig_Enabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	os.WriteFile(path, []byte("[cache]\nenabled = yes\n"), 0600)

	cfg := ReadCacheConfig(path)
	if !cfg.Enabled {
		t.Error("expected enabled=true when set to yes")
	}
}

func TestReadCacheConfig_MissingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	os.WriteFile(path, []byte("[hooks]\nPreToolUse = yes\n"), 0600)

	cfg := ReadCacheConfig(path)
	if !cfg.Enabled {
		t.Error("missing [cache] section should default to enabled")
	}
}

func TestReadCacheConfig_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	os.WriteFile(path, []byte("[cache]\nEnabled = NO\n"), 0600)

	cfg := ReadCacheConfig(path)
	if cfg.Enabled {
		t.Error("expected case-insensitive 'NO' to disable")
	}
}
