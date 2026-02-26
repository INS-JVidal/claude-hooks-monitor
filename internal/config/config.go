package config

import (
	"os"
	"path/filepath"
	"strings"
)

// HookEntry represents a single hook's name and enabled state.
type HookEntry struct {
	Name    string
	Enabled bool
}

// HookConfig holds the ordered list of hook entries from the [hooks] section.
type HookConfig struct {
	Hooks []HookEntry
}

// AllHookTypes lists the 15 canonical hook type names in display order.
// Order matches hook_monitor.conf for consistency with user-facing config.
var AllHookTypes = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"PermissionRequest",
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

// ReadConfig reads the INI file and returns the state of all 15 hooks.
// Missing file or missing keys default to enabled (fail-open).
// Parsing matches the hook-client's isHookEnabled behavior:
//   - UTF-8 BOM stripping
//   - Case-insensitive section and key matching
//   - Inline comment stripping ("yes # comment" -> "yes")
//   - Last-wins for duplicate keys
func ReadConfig(path string) (HookConfig, error) {
	parsed, err := parseINIHooks(path)

	cfg := HookConfig{Hooks: make([]HookEntry, len(AllHookTypes))}
	for i, name := range AllHookTypes {
		enabled := true
		if val, ok := parsed[strings.ToLower(name)]; ok {
			enabled = !strings.EqualFold(val, "no")
		}
		cfg.Hooks[i] = HookEntry{Name: name, Enabled: enabled}
	}
	return cfg, err
}

// WriteConfig writes the [hooks] section atomically (temp + rename), preserving
// any other sections (e.g., [sink]) that exist in the file. If the file doesn't
// exist yet, only the [hooks] section is written.
func WriteConfig(path string, cfg HookConfig) error {
	var hooks strings.Builder
	hooks.WriteString("[hooks]\n")
	hooks.WriteString("# Toggle individual hooks on/off.\n")
	hooks.WriteString("# Set to \"yes\" to monitor, \"no\" to skip.\n")
	hooks.WriteString("# Missing entries default to \"yes\" (monitor everything).\n")
	hooks.WriteString("# Changes take effect immediately — no restart needed.\n")
	hooks.WriteString("\n")

	for _, h := range cfg.Hooks {
		val := "yes"
		if !h.Enabled {
			val = "no"
		}
		hooks.WriteString(h.Name + " = " + val + "\n")
	}

	// Preserve non-[hooks] sections from the existing file.
	other := extractNonHooksContent(path)
	var b strings.Builder
	b.WriteString(hooks.String())
	if other != "" {
		b.WriteString("\n")
		b.WriteString(other)
	}

	return AtomicWriteFile(path, []byte(b.String()), 0600)
}

// extractNonHooksContent reads the file at path and returns all lines that
// belong to sections other than [hooks] (including their section headers,
// comments, and blank lines). Returns "" if the file is missing or contains
// only the [hooks] section.
func extractNonHooksContent(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimPrefix(string(raw), "\xef\xbb\xbf")

	var result strings.Builder
	inHooks := false
	seenNonHooks := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			inHooks = strings.EqualFold(trimmed, "[hooks]")
			if !inHooks {
				seenNonHooks = true
			}
		}
		if !inHooks && seenNonHooks {
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}

	return strings.TrimRight(result.String(), "\n") + "\n"
}

// IsEnabled checks whether a single hook is enabled in the config.
// Fail-open: missing file, missing key, or any error -> true.
// Key matching is case-insensitive.
func IsEnabled(path, hookName string) bool {
	parsed, _ := parseINIHooks(path)
	if val, ok := parsed[strings.ToLower(hookName)]; ok {
		return !strings.EqualFold(val, "no")
	}
	return true
}

// SinkConfig holds the configuration for forwarding events to an external sink.
type SinkConfig struct {
	Forward  bool   // Whether to forward events.
	Endpoint string // Target URL (e.g., "http://localhost:9800/ingest").
}

// ReadSinkConfig reads the [sink] section from the INI config file.
// Missing file or missing section defaults to forwarding disabled.
func ReadSinkConfig(path string) SinkConfig {
	parsed, _ := parseINISection(path, "sink")
	cfg := SinkConfig{
		Endpoint: "http://localhost:9800/ingest",
	}
	if val, ok := parsed["forward"]; ok {
		cfg.Forward = strings.EqualFold(val, "yes")
	}
	if val, ok := parsed["endpoint"]; ok && val != "" {
		cfg.Endpoint = val
	}
	return cfg
}

// CacheConfig holds the configuration for file read caching.
type CacheConfig struct {
	Enabled bool // Whether to track file reads for dedup annotations.
}

// ReadCacheConfig reads the [cache] section from the INI config file.
// Missing file, missing section, or missing key defaults to enabled (fail-open).
func ReadCacheConfig(path string) CacheConfig {
	parsed, _ := parseINISection(path, "cache")
	cfg := CacheConfig{Enabled: true}
	if val, ok := parsed["enabled"]; ok {
		cfg.Enabled = !strings.EqualFold(val, "no")
	}
	return cfg
}

// parseINISection reads a named section from an INI file and returns
// a map of lowercased key -> raw value. Returns an empty map (not an error)
// if the file is missing, unreadable, or the section doesn't exist.
func parseINISection(path, section string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}, err
	}

	content := string(raw)
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")

	target := "[" + strings.ToLower(section) + "]"
	result := make(map[string]string)
	inSection := false

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			inSection = strings.EqualFold(line, target)
			continue
		}
		if !inSection {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if idx := strings.Index(val, "#"); idx >= 0 {
			val = strings.TrimSpace(val[:idx])
		}
		result[strings.ToLower(key)] = val
	}
	return result, nil
}

// parseINIHooks reads the [hooks] section from an INI file and returns
// a map of lowercased key -> raw value. Returns an empty map (not an error)
// if the file is missing or unreadable, preserving fail-open semantics.
func parseINIHooks(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}, err
	}

	content := string(raw)
	// Strip UTF-8 BOM if present (Windows editors add this).
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")

	result := make(map[string]string)
	inHooksSection := false

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

		// Strip inline comments: "yes # enable this" -> "yes"
		if idx := strings.Index(val, "#"); idx >= 0 {
			val = strings.TrimSpace(val[:idx])
		}

		// Store lowercased key, last-wins for duplicates.
		result[strings.ToLower(key)] = val
	}

	return result, nil
}

// AtomicWriteFile writes data to a temp file and renames it to the target path.
// Readers never see a partial file — they get either the old content or the new.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
