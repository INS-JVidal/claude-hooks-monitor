# Claude Code Hooks Monitor

A real-time monitoring and logging system for Claude Code CLI hooks. Watch every hook event as it fires — colorized in your terminal **or explore interactively in a tree UI** — to understand how Claude Code's hook system works.

## Features

- Captures all 14 Claude Code hook event types (+ SessionEnd)
- **Interactive tree UI** (`--ui` flag) — collapsible Session → Request → Tool hierarchy
- Vim-style keyboard navigation (j/k/h/l, g/G, space toggle)
- Auto-scroll follows new events in real time
- Pre/Post tool event pairing — PreToolUse automatically links to its PostToolUse result
- Colorized console output — each hook type gets a unique color
- REST API with `/stats`, `/events`, `/health` endpoints
- Per-hook toggle config — enable/disable individual hooks without restarting
- Non-blocking — hooks always exit 0, never interfere with Claude
- Bounded memory — ring buffer caps event history at 1000, with proper GC-friendly compaction
- Non-blocking TUI channel with dropped event counter in the header
- Unified graceful shutdown — both console and TUI modes clean up lock files, port files, and HTTP server
- Single-instance guard — file lock prevents duplicate monitors with informative diagnostics
- Go hook-client binary — fast, single-shot forwarder with 2s timeout
- Python hook script alternative (reads `hook_event_name` from stdin)
- End-to-end test suite with 3 test phases

## Prerequisites

- **Go** 1.24+ — [golang.org/dl](https://go.dev/dl/)
- **Python** 3.11+
- **uv** — [docs.astral.sh/uv](https://docs.astral.sh/uv/)
- **Claude Code CLI** — latest version

## Quick Start

**Step 1: Install dependencies** (~1 minute)

```bash
cd claude-hooks-monitor
make deps
```

**Step 2: Start the monitor** (~30 seconds)

```bash
# Terminal 1: run the server (console mode)
make run

# Or: interactive tree UI
make run-ui
```

**Step 3: Test it** (~1 minute)

```bash
# Terminal 2: run the test suite
make test
```

Or use it with Claude Code directly:

```bash
# Terminal 2: start Claude in this project directory
claude
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/hook/{HookType}` | Receive a hook event |
| GET | `/stats` | Aggregate hook counts |
| GET | `/events` | Last 100 events (use `?limit=N`) |
| GET | `/health` | Health check |

## Hook Types

All 14 configurable Claude Code hook events:

| # | Event | When it fires | Has Matcher? |
|---|-------|---------------|:---:|
| 1 | SessionStart | Session begins | No |
| 2 | UserPromptSubmit | User sends a prompt | No |
| 3 | PreToolUse | Before tool execution | Yes |
| 4 | PermissionRequest | Claude needs permission | No |
| 5 | PostToolUse | After tool succeeds | Yes |
| 6 | PostToolUseFailure | After tool fails | Yes |
| 7 | Notification | System notification | No |
| 8 | SubagentStart | Subagent launched | No |
| 9 | SubagentStop | Subagent completes | No |
| 10 | Stop | Claude stops responding | No |
| 11 | TeammateIdle | Teammate agent idle | No |
| 12 | TaskCompleted | Task completes | No |
| 13 | ConfigChange | Config changes | No |
| 14 | PreCompact | Before context compaction | No |

Plus **SessionEnd** (fires on session termination).

## Hook Stdout Semantics

This project is a **passive monitor** — it produces no stdout. But understanding what hooks *can* do is key:

| Mechanism | Effect |
|-----------|--------|
| Exit 0 | Success. Stdout JSON processed for decisions. |
| Exit 2 | Blocking error. Stderr fed to Claude. Blocks the action. |
| `{"decision": "block", "reason": "..."}` | Denies the action (PreToolUse denies tool, Stop forces continue). |
| `{"continue": false}` | Immediately stops Claude. |
| PreToolUse `updatedInput` | Modifies tool input before execution. |
| SessionStart stdout | Added to Claude's context. |

## Usage Examples

```bash
# Start server on custom port
PORT=9000 make run

# Send a single test hook manually
make send-test-hook

# Check statistics
make stats

# View last 5 events
curl http://localhost:8080/events?limit=5 | python3 -m json.tool
```

## Tree UI Mode

The `--ui` flag launches an interactive terminal UI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Events are organized into a collapsible tree:

```
Session (by session_id)
 └─ Request (each UserPromptSubmit)
     ├─ PreToolUse: Bash → echo hello
     │   └─ PostToolUse: Bash completed     ← auto-paired
     ├─ PreToolUse: Read → /src/main.go
     │   └─ PostToolUseFailure: Read FAILED  ← auto-paired
     ├─ Notification
     └─ Stop
```

**Start the TUI:**

```bash
./bin/monitor --ui          # after 'make build'
make run-ui                 # build + run in one step
PORT=9000 make run-ui       # custom port
```

**Key bindings:**

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `l` / `→` / `Enter` | Expand node |
| `h` / `←` | Collapse node |
| `Space` | Toggle expand/collapse |
| `g` | Jump to top |
| `G` | Jump to bottom (re-enables auto-scroll) |
| `q` / `Ctrl+C` | Quit |

The header shows the port, total event count, and a dropped event counter (visible only if the TUI can't keep up with the event rate).

## Testing

The test suite has 3 phases:

1. **Direct server test** — curls all 17 hook payloads directly to the server
2. **End-to-end test** — pipes JSON through the Python script and verifies it reaches the server with no stdout
3. **Config toggle test** — disables a hook in config, verifies it's skipped, restores config

```bash
make test
```

## Configuration

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `PORT` | 8080 | Server listen port |
| `HOOK_MONITOR_URL` | http://localhost:8080 | Server URL (for Python script) |
| `HOOK_TIMEOUT` | 2 | HTTP timeout in seconds |

### Hook Toggle (hooks/hook_monitor.conf)

Toggle individual hooks on/off without editing settings.json:

```ini
[hooks]
SessionStart = yes
PreToolUse = no       # disable noisy tool hooks
PostToolUse = no
PostToolUseFailure = yes
# ... etc
```

Changes take effect immediately. View current state: `make show-config`. Reset all: `make reset-config`.

## Troubleshooting

**Server won't start:**
- Check port isn't in use: `lsof -i:8080`
- Try a different port: `PORT=9000 make run`

**Hooks don't fire:**
- Verify Claude sees them: run `/hooks` inside Claude Code
- Check script is executable: `ls -la hooks/hook_monitor.py`
- Verify settings.json: `cat .claude/settings.json | python3 -m json.tool`

**Hook fires but no event in server:**
- Is the server running? `make check`
- Is the hook enabled? `make show-config`
- Test manually: `echo '{"hook_event_name":"Test"}' | ./hooks/hook_monitor.py`

**Config changes don't take effect:**
- Check section header is `[hooks]` (not `[Hooks]` or missing)
- Values must be exactly `yes` or `no`

## Learning Guide

### Exercises

1. **Observe a session lifecycle:** Start the monitor, launch Claude, ask a question, exit. Watch SessionStart → UserPromptSubmit → PreToolUse → PostToolUse → Stop → SessionEnd.

2. **Disable noisy hooks:** Set `PreToolUse = no` and `PostToolUse = no` in config. Now you only see high-level events.

3. **Add a new endpoint:** Add `GET /events/{hookType}` that returns events filtered by type.

4. **Count tool usage:** Use `/stats` to see which tools Claude uses most during a session.

5. **Understand blocking:** Create a test hook that exits with code 2 and observe how Claude handles it (in a test project — not this one!).

## Project Structure

```
claude-hooks-monitor/
├── .claude/
│   └── settings.json            # Hook config + permissions
├── cmd/
│   └── hook-client/main.go      # Go hook client — single-shot HTTP forwarder
├── internal/
│   └── hookevt/hookevt.go       # Shared HookEvent type (used by monitor + TUI)
├── tui/
│   ├── model.go                 # Bubble Tea model, key handling, viewport
│   ├── tree.go                  # Tree data structures (Session/Request/EventNode)
│   ├── processor.go             # Event → tree builder with Pre/Post pairing
│   └── styles.go                # Lipgloss styles and row rendering
├── main.go                      # Entrypoint — flag parsing, HTTP setup, mode dispatch
├── monitor.go                   # HookMonitor — event buffer, stats, TUI channel
├── server.go                    # HTTP handlers (/hook, /stats, /events, /health)
├── lock.go                      # Single-instance file lock with diagnostics
├── hooks/
│   ├── hook-client              # Compiled Go hook client binary
│   ├── hook_monitor.py          # Alternative Python hook script
│   └── hook_monitor.conf        # Toggle: enable/disable hooks
├── plans/                       # Development planning documents
├── go.mod / go.sum              # Go dependencies (bubbletea, lipgloss, fatih/color)
├── Makefile                     # Build automation (run, run-ui, test, etc.)
├── test-hooks.sh                # Test suite (3 phases)
├── README.md                    # This file
├── EXAMPLES.md                  # Output examples
└── ARCHITECTURE.md              # Architecture deep-dive
```

## Performance

The codebase is optimized for minimal impact on Claude Code responsiveness:

- **Ring buffer compaction** — uses `copy()` into fresh slices so the GC can reclaim old backing arrays (avoids the classic Go slice-pinning memory leak)
- **Lock-free logging** — event logging (JSON marshal + terminal I/O) happens outside the mutex, so concurrent API readers are never blocked by console output
- **Event channel ordering** — channel send is inside the mutex, guaranteeing TUI receives events in insertion order
- **Non-blocking TUI channel** — buffered channel (256) with `select`/`default` drop; counter tracks dropped events in the TUI header
- **Pre-computed colors** — hook type colors are allocated once at startup in a map, not on every event
- **Bounded I/O** — both server and hook-client cap request/stdin reads at 1 MiB via `io.LimitReader`
- **Single-shot HTTP** — hook-client disables keep-alive since it's a short-lived process (one request, then exit)
- **UTF-8 safe truncation** — string truncation respects rune boundaries to avoid producing invalid UTF-8
- **2-second timeout** — hook-client times out quickly if the monitor is unreachable; connection-refused returns in milliseconds
- **Unified shutdown** — context cancellation + deferred cleanup ensures lock files, port files, and HTTP server are released in both console and TUI modes

## Next Steps

Ideas for extending this project:

- **TUI detail pane** — split view showing full event JSON for the selected node
- **TUI search/filter** — `/` to filter events by hook type or tool name
- Add a **web dashboard** with real-time WebSocket updates
- **Persist events** to SQLite for post-session analysis
- **Session timeline** — visual duration bars for tool execution (Pre→Post delta)
- Export to **CSV/JSON** for data analysis
- Add **Prometheus metrics** for monitoring at scale

## Resources

- [Claude Code Hooks Documentation](https://docs.claude.com/en/docs/claude-code/hooks)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework (The Elm Architecture for Go)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — TUI styling
- [Go fatih/color](https://github.com/fatih/color) — console colorization
- [uv Documentation](https://docs.astral.sh/uv/)
- [Python requests](https://requests.readthedocs.io/)

## License

MIT
