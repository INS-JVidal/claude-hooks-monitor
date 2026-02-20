# Architecture — Claude Code Hooks Monitor

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                      Claude Code CLI                            │
│  Hook Event Fires → Executes Python Script                     │
│  Provides JSON on stdin with common + event-specific fields    │
└─────────────────────┬───────────────────────────────────────────┘
                      │ stdin (JSON)
                      ↓
┌─────────────────────────────────────────────────────────────────┐
│            hook_monitor.py (Python Script)                      │
│  1. Read hook_event_name from stdin JSON                       │
│  2. Check hook_monitor.conf — is this hook enabled?            │
│     • If disabled → exit 0 immediately (skip)                  │
│     • If enabled  → continue                                   │
│  3. Add _monitor metadata                                      │
│  4. HTTP POST to server                                        │
│  5. Exit 0 (non-blocking, no stdout)                           │
└─────────────────────┬───────────────────────────────────────────┘
                      │ HTTP POST (only if enabled)
                      ↓
┌─────────────────────────────────────────────────────────────────┐
│              Go REST API Server (main.go)                       │
│  Ring buffer (max 1000 events) + stats counters                │
│  Thread-safe via sync.RWMutex                                  │
│  Colorized console output per hook type                        │
└─────────────────────────────────────────────────────────────────┘
```

## Data Flow

### Step-by-step

1. **Claude Code fires a hook** — e.g., PreToolUse before writing a file
2. **Python script executes** — receives JSON on stdin with `hook_event_name`, `session_id`, `cwd`, etc.
3. **Config check** — reads `hooks/hook_monitor.conf` via `configparser`
   - Disabled → exit 0, no network call
   - Enabled → continue
4. **Metadata enrichment** — adds `_monitor` dict with UTC timestamp, project dir
5. **HTTP POST** — sends to `http://localhost:8080/hook/{hook_event_name}`
6. **Server receives** — parses JSON, creates `HookEvent`, stores in ring buffer
7. **Console output** — colorized log with separator, hook type, timestamp, JSON data
8. **Script exits 0** — Claude Code continues without delay

### Stdin JSON Schema

Every hook receives these common fields:

```json
{
  "session_id": "abc123-def456",
  "transcript_path": "/home/user/.claude/projects/.../transcript.jsonl",
  "cwd": "/home/user/my-project",
  "permission_mode": "default",
  "hook_event_name": "PreToolUse"
}
```

Event-specific fields are merged alongside (e.g., `tool_name`, `tool_input` for PreToolUse).

## Component Details

### Go Server (main.go)

**Data structures:**

```go
type HookEvent struct {
    HookType  string                 `json:"hook_type"`
    Timestamp time.Time              `json:"timestamp"`
    Data      map[string]interface{} `json:"data"`
}

type HookMonitor struct {
    events []HookEvent      // ring buffer, max 1000
    mu     sync.RWMutex     // protects events + stats
    stats  map[string]int   // hook_type → count
}
```

**Thread safety:** All access to `events` and `stats` goes through `mu`. Writes use `Lock()`, reads use `RLock()`. Deferred `Unlock()` ensures no deadlocks.

**Ring buffer:** When `len(events) > 1000`, the oldest 100 are dropped at once. This amortizes the cost of trimming rather than shifting on every insert.

**Endpoint registration:** Each hook type is registered via `http.HandleFunc("/hook/"+hookType, handleHook(monitor, hookType))`. The closure captures the hook type, so one handler function serves all 15 endpoints.

### Python Script (hooks/hook_monitor.py)

**uv single-file script:** The shebang `#!/usr/bin/env -S uv run --quiet --script` plus the PEP 723 metadata block lets `uv` automatically manage dependencies. No virtualenv setup needed.

**Config toggle:** `is_hook_enabled()` uses `configparser` with `optionxform = str` (critical — default lowercases keys, breaking PascalCase hook names). Fail-open design: any error returns True.

**Safety guarantees:**
- Never writes to stdout (stdout = hook decision channel)
- Always exits 0 (exit 2 = blocking error in Claude Code)
- Top-level `try/except` catches everything
- Connection errors fail silently (server may not be running)

### Config File (hooks/hook_monitor.conf)

INI format read by Python's `configparser` (stdlib). No extra dependencies.

```ini
[hooks]
SessionStart = yes
PreToolUse = no
```

Read fresh on every invocation (hooks are short-lived processes). Edits take effect immediately.

## Environment Variables

| Variable | Default | Used By | Purpose |
|----------|---------|---------|---------|
| `PORT` | 8080 | Go server | Listen port |
| `HOOK_MONITOR_URL` | http://localhost:8080 | Python script | Server URL |
| `HOOK_TIMEOUT` | 5 | Python script | HTTP timeout (seconds) |
| `CLAUDE_PROJECT_DIR` | (set by Claude) | settings.json | Project root path |
| `CLAUDE_PLUGIN_ROOT` | (set by Claude) | Python script | Plugin context |
| `CLAUDE_CODE_REMOTE` | (set by Claude) | Python script | Remote session flag |

## Hook Lifecycle

### PreToolUse/PostToolUse Flow

```
Claude wants to use Write tool
  │
  ├─ PreToolUse hook fires
  │   ├─ stdin: {hook_event_name: "PreToolUse", tool_name: "Write", tool_input: {...}}
  │   ├─ hook_monitor.py reads stdin, checks config
  │   ├─ POSTs to server (if enabled)
  │   └─ exits 0 (no stdout = no decision = allow)
  │
  ├─ Claude executes the Write tool
  │
  └─ PostToolUse hook fires
      ├─ stdin: {hook_event_name: "PostToolUse", tool_name: "Write", tool_response: {...}}
      ├─ hook_monitor.py reads stdin, checks config
      ├─ POSTs to server (if enabled)
      └─ exits 0
```

### Hook Decision Model (educational reference)

Hooks can influence Claude via stdout and exit codes:

```
Hook script runs
  │
  ├─ exit 0 + no stdout     → allow (what this monitor does)
  ├─ exit 0 + decision JSON → allow/deny/modify based on JSON
  ├─ exit 2 + stderr        → block action, feed error to Claude
  └─ exit other + stderr    → non-blocking error (verbose mode only)
```

## Security Notes

- **Localhost only:** Server binds to `localhost`. No authentication.
- **No data persistence:** Events are in-memory only. Lost on restart.
- **Stdin contains sensitive data:** Tool inputs may include file contents, commands, etc. The monitor logs everything to console — do not run in shared/public environments.
- **Hook scripts run as your user:** They have full filesystem access. Review any hook script before enabling it.

## Extension Points

| What | How |
|------|-----|
| Add persistence | Store `HookEvent` to SQLite in `AddEvent()` |
| Add web dashboard | Serve HTML from Go, add WebSocket for real-time updates |
| Filter events | Add query params to `/events` (e.g., `?type=PreToolUse`) |
| Add authentication | Middleware in Go's HTTP handler chain |
| Export data | Add `/export` endpoint returning CSV or JSONL |
| Multi-project | Add project ID to `HookEvent`, partition storage |

## Performance

**Python script overhead:** ~50-100ms per invocation (uv startup + HTTP POST). Non-blocking — Claude doesn't wait for it.

**Go server throughput:** Handles 1000+ req/sec easily. The bottleneck is console I/O (printing), not computation.

**Memory:** Ring buffer capped at 1000 events. Each event is ~1-5 KB depending on payload. Max memory: ~5 MB for events.

**Config read:** `configparser.read()` on every invocation adds ~1ms. Negligible vs. HTTP overhead.
