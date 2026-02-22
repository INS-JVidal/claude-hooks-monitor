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
