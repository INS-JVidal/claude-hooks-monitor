# cmd/hook-client — Claude Code hook handler binary

All files stable — prefer this summary over reading source files.

## main.go

Runs per-hook-event. Reads JSON from stdin (1 MiB max), extracts hook_event_name, validates hookType (alpha-only for path safety), checks toggle config, enriches with _monitor metadata, POSTs to monitor.

The `_monitor` metadata includes: timestamp, project_dir, plugin_root, is_remote, has_claude_md. The `has_claude_md` flag checks via `os.Stat` whether `CLAUDE.md` exists in the project's `cwd` directory.

**File cache integration:** On PostToolUse Read events, `enrichPostToolUseCache` stats the file and adds `_cache` metadata (file_path, mtime_ns, size) for the monitor's SessionFileCache. On PreToolUse Read events, `handlePreToolUseRead` queries the monitor's `/cache/file` endpoint, stats the file, and if mtime+size are unchanged, writes a `hookSpecificOutput` JSON to stdout with an `additionalContext` annotation suggesting Claude skip the re-read.

Subcommand: `install-hooks` — registers all hooks in ~/.claude/settings.json (idempotent).

Config discovery: env var → project-level (.claude/) → XDG dir → binary-relative. Monitor URL: env var → XDG port file → binary-relative port file. Always exits 0 to never block Claude.

Key unexported funcs: isAlphaOnly (security validation), isHookEnabled (INI parser, fail-open), discoverMonitorURL (loopback-only URL validation), truncate (UTF-8 safe), hasClaudeMD (CLAUDE.md existence check), enrichPostToolUseCache (PostToolUse Read file stat), handlePreToolUseRead (PreToolUse Read cache query + annotation), queryFileCache (HTTP GET to monitor).

## client_test.go

Tests: isAlphaOnly (path traversal, injection, unicode), truncate (UTF-8 boundary), discoverMonitorURL (SSRF prevention, port file), isHookEnabled (INI parsing edge cases), hasClaudeMD (exists, not exists, missing/empty/non-string cwd), sendToMonitor (happy path, auth, server down), enrichPostToolUseCache (Read/non-Read/missing input/nonexistent file), handlePreToolUseRead (unchanged/changed/not cached/non-Read/no session/monitor down).

Imports: `config` (AllHookTypes, AtomicWriteFile), `filecache` (CacheQuery).
