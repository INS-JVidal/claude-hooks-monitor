package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ============================================================
// AtomicWriteFile
// ============================================================

func TestAtomicWrite_CreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "newfile.txt")

	if err := AtomicWriteFile(path, []byte("hello world"), 0600); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

func TestAtomicWrite_OverwritesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	AtomicWriteFile(path, []byte("first"), 0600)
	AtomicWriteFile(path, []byte("second"), 0600)

	data, _ := os.ReadFile(path)
	if string(data) != "second" {
		t.Errorf("content = %q, want %q", data, "second")
	}
}

func TestAtomicWrite_Permissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secure.txt")

	if err := AtomicWriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

func TestAtomicWrite_NoTempLeftover(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	AtomicWriteFile(path, []byte("data"), 0600)

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestAtomicWrite_InvalidDir(t *testing.T) {
	t.Parallel()
	err := AtomicWriteFile("/nonexistent/dir/file.txt", []byte("data"), 0600)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestAtomicWrite_ConcurrentReads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.txt")

	// Write initial content
	AtomicWriteFile(path, []byte("initial"), 0600)

	var wg sync.WaitGroup

	// Writer: continuously update the file
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; i < 100; i++ {
			content := strings.Repeat("X", 100) // consistent length
			AtomicWriteFile(path, []byte(content), 0600)
		}
	}()

	// Readers: read concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				data, err := os.ReadFile(path)
				if err != nil {
					continue // file might be briefly missing during rename
				}
				// Should never see partial content
				content := string(data)
				if content != "initial" && content != strings.Repeat("X", 100) {
					t.Errorf("read partial content: %q (len=%d)", content[:min(20, len(content))], len(content))
				}
			}
		}()
	}

	<-writerDone
	wg.Wait()
}

// ============================================================
// ReadConfig / WriteConfig Edge Cases
// ============================================================

func TestReadConfig_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.conf")
	os.WriteFile(path, []byte(""), 0600)

	cfg, _ := ReadConfig(path)
	for _, h := range cfg.Hooks {
		if !h.Enabled {
			t.Errorf("hook %s should default to enabled for empty file", h.Name)
		}
	}
}

func TestReadConfig_NoHooksSection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	os.WriteFile(path, []byte("[other]\nFoo = bar\n"), 0600)

	cfg, _ := ReadConfig(path)
	for _, h := range cfg.Hooks {
		if !h.Enabled {
			t.Errorf("hook %s should default to enabled without [hooks] section", h.Name)
		}
	}
}

func TestReadConfig_MultipleSections(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	// First [hooks] sets no, [other] section, second [hooks] sets yes
	content := "[hooks]\nPreToolUse = no\n[other]\nFoo = bar\n[hooks]\nPreToolUse = yes\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, _ := ReadConfig(path)
	for _, h := range cfg.Hooks {
		if h.Name == "PreToolUse" && !h.Enabled {
			t.Error("second [hooks] section should override PreToolUse to yes")
		}
	}
}

func TestReadConfig_WindowsLineEndings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\r\nPreToolUse = no\r\nPostToolUse = yes\r\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, _ := ReadConfig(path)
	lookup := make(map[string]bool)
	for _, h := range cfg.Hooks {
		lookup[h.Name] = h.Enabled
	}
	if lookup["PreToolUse"] {
		t.Error("PreToolUse should be disabled (Windows line endings)")
	}
	if !lookup["PostToolUse"] {
		t.Error("PostToolUse should be enabled (Windows line endings)")
	}
}

func TestReadConfig_TrailingWhitespace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]  \n  PreToolUse  =  no  \n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, _ := ReadConfig(path)
	for _, h := range cfg.Hooks {
		if h.Name == "PreToolUse" && h.Enabled {
			t.Error("PreToolUse should be disabled (extra whitespace)")
		}
	}
}

func TestReadConfig_NoEqualsSign(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nPreToolUse\nPostToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, _ := ReadConfig(path)
	lookup := make(map[string]bool)
	for _, h := range cfg.Hooks {
		lookup[h.Name] = h.Enabled
	}
	if !lookup["PreToolUse"] {
		t.Error("PreToolUse (no equals) should default to enabled")
	}
	if lookup["PostToolUse"] {
		t.Error("PostToolUse should be disabled")
	}
}

func TestReadConfig_ValueWithEquals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	// "key = a=b" → value is "a=b", which is not "no" → enabled
	content := "[hooks]\nPreToolUse = a=b\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, _ := ReadConfig(path)
	for _, h := range cfg.Hooks {
		if h.Name == "PreToolUse" && !h.Enabled {
			t.Error("PreToolUse with value 'a=b' should be enabled (not 'no')")
		}
	}
}

func TestReadConfig_VeryLongLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	longVal := strings.Repeat("x", 100000)
	content := "[hooks]\nPreToolUse = " + longVal + "\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, _ := ReadConfig(path)
	// Should not crash, PreToolUse should be enabled (long value != "no")
	for _, h := range cfg.Hooks {
		if h.Name == "PreToolUse" && !h.Enabled {
			t.Error("PreToolUse with long value should be enabled")
		}
	}
}

func TestWriteConfig_ReadableOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")

	cfg := HookConfig{Hooks: []HookEntry{{Name: "SessionStart", Enabled: true}}}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "[hooks]") {
		t.Error("output should contain [hooks] header")
	}
	if !strings.Contains(content, "Changes take effect immediately") {
		t.Error("output should contain user-friendly comments")
	}
	if !strings.Contains(content, "SessionStart = yes") {
		t.Error("output should contain SessionStart = yes")
	}
}

// ============================================================
// parseINIHooks Adversarial Inputs
// ============================================================

func TestParseINI_MalformedSection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	// "[hooks" without closing bracket — treated as a section check, fails EqualFold
	content := "[hooks\nPreToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	// "[hooks" starts with '[' so it enters section parsing
	// EqualFold("[hooks", "[hooks]") is false → not in hooks section
	// PreToolUse line is outside hooks section → enabled
	if !IsEnabled(path, "PreToolUse") {
		t.Error("malformed section should not match [hooks]")
	}
}

func TestParseINI_EmptySection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[]\nPreToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	if !IsEnabled(path, "PreToolUse") {
		t.Error("empty section [] should not match [hooks]")
	}
}

func TestParseINI_OnlyComments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "# comment 1\n# comment 2\n# comment 3\n"
	os.WriteFile(path, []byte(content), 0600)

	if !IsEnabled(path, "PreToolUse") {
		t.Error("file with only comments should default to enabled")
	}
}

func TestParseINI_BinaryGarbage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	garbage := []byte{0x00, 0xFF, 0xFE, 0x01, 0x89, 0x50, 0x4E, 0x47}
	os.WriteFile(path, garbage, 0600)

	// Should not crash, should default to enabled
	if !IsEnabled(path, "PreToolUse") {
		t.Error("binary garbage should default to enabled")
	}
}

// ============================================================
// AllHookTypes completeness
// ============================================================

func TestAllHookTypes_Count(t *testing.T) {
	t.Parallel()
	if len(AllHookTypes) != 15 {
		t.Errorf("AllHookTypes has %d entries, want 15", len(AllHookTypes))
	}
}

func TestAllHookTypes_NoDuplicates(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for _, ht := range AllHookTypes {
		if seen[ht] {
			t.Errorf("duplicate hook type: %s", ht)
		}
		seen[ht] = true
	}
}

// ============================================================
// WriteConfig → ReadConfig round-trip with all disabled
// ============================================================

func TestWriteConfig_AllDisabled_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")

	original := HookConfig{Hooks: make([]HookEntry, len(AllHookTypes))}
	for i, name := range AllHookTypes {
		original.Hooks[i] = HookEntry{Name: name, Enabled: false}
	}

	if err := WriteConfig(path, original); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range cfg.Hooks {
		if h.Enabled {
			t.Errorf("hook %s should be disabled after round-trip", h.Name)
		}
	}
}

// ============================================================
// IsEnabled — case sensitivity edge cases
// ============================================================

func TestIsEnabled_AllCaseVariants(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.conf")
	content := "[hooks]\nPreToolUse = no\n"
	os.WriteFile(path, []byte(content), 0600)

	variants := []string{"PreToolUse", "pretooluse", "PRETOOLUSE", "pReToOlUsE"}
	for _, v := range variants {
		if IsEnabled(path, v) {
			t.Errorf("IsEnabled(%q) should be false (case-insensitive)", v)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
