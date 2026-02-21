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

- **Claude Code CLI** — latest version
- **Go** 1.21+ ([go.dev/dl](https://go.dev/dl/)) — required to build from source
- **Git** — required to clone the repository
- **Make** — optional (needed for `make run`, `make run-ui`, etc.; not needed for building)
- *Optional:* Python 3.11+, uv, jq (for test suite and alternative hook script)

## Installation

### Linux / macOS

**One-line install** (clones and builds from source):

```bash
curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

On macOS with Homebrew, the installer automatically installs missing Go/Git dependencies.

Custom install location:

```bash
INSTALL_DIR=~/projects/hooks-monitor \
  curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

### Windows

**PowerShell installer** (clones and builds from source):

```powershell
Invoke-WebRequest -Uri "https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.ps1" -OutFile install.ps1
.\install.ps1
```

Requires Go and Git. If missing, the script suggests `winget install` commands.

### Manual install (all platforms)

```bash
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
make build          # or: go build -ldflags="-s -w" -o bin/monitor . && go build -ldflags="-s -w" -o hooks/hook-client ./cmd/hook-client
```

**System dependencies (Ubuntu/Debian only):**

If you need Go, Python, uv, and other tools installed:

```bash
# Run from inside the repo, or standalone:
bash setup.sh
```

> For detailed per-platform instructions, see [INSTALLME.md](INSTALLME.md).

## Quick Start

```bash
# Terminal 1: start the monitor
cd claude-hooks-monitor
make run            # console mode
# Or: make run-ui  # interactive tree UI

# Terminal 2: test it
make test           # run the 3-phase test suite
# Or: claude        # start Claude in this project — hooks fire automatically
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

## Configuring Hooks

### Using with this project

No configuration needed — just `make run` in one terminal and `claude` in another. The included `.claude/settings.json` wires all 15 hook types to the monitor automatically.

### Using with your own project

To monitor hooks while working in a different project:

1. Generate the hooks config with absolute paths:
   ```bash
   cd ~/claude-hooks-monitor
   make show-hooks-config
   ```
2. Copy the JSON output into your project's `.claude/settings.json`.
3. Start the monitor (`make run`) and then `claude` in your project.

> See [INSTALLME.md](INSTALLME.md#configuring-claude-code-hooks) for a full walkthrough and example JSON.

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

## Usage Guide

### Starting the monitor

```bash
make run            # console mode (colorized output)
make run-ui         # interactive tree UI
make run-background # background mode (logs to monitor.log)
```

### Custom port

```bash
PORT=9000 make run
PORT=9000 make run-ui
```

### Running with Claude Code

```bash
# Terminal 1
make run

# Terminal 2 — in this project or your own (if hooks are configured)
claude
```

### Slash command (from within Claude Code)

When running `claude` inside the monitor project, the `/monitor-hooks` slash command is available to toggle hook monitoring:

```
/monitor-hooks activate             # Enable all hooks
/monitor-hooks deactivate           # Disable all hooks
/monitor-hooks status               # Show monitor state + per-hook config
/monitor-hooks show-all             # Audit: compare known hooks vs config
/monitor-hooks PreToolUse off       # Disable a specific hook
/monitor-hooks PreToolUse on        # Enable a specific hook
/monitor-hooks help                 # Show usage and valid hook types
```

Hook names are case-insensitive. Changes take effect immediately (the hook-client reads the config on every invocation).

To use `/monitor-hooks` from a different project:

```bash
cd ~/claude-hooks-monitor
make install-command PROJECT=~/my-project
```

This copies the slash command with absolute paths baked in, so it works from any project directory.

**Permissions:** Claude Code will ask for confirmation the first time you run `/monitor-hooks`. To auto-approve, add this to your project's `.claude/settings.json`:

```json
{
  "permissions": {
    "allowedTools": ["Bash(*monitor-hooks*)"]
  }
}
```

Alternatively, use `claude --allowedTools 'Bash(*monitor-hooks*)'` when launching, or simply press `y` when prompted (Claude Code may remember the approval for the session).

### Checking statistics

```bash
make stats                              # aggregate hook counts
make check                              # is the server running?
curl http://localhost:8080/events?limit=5 | python3 -m json.tool  # last 5 events
```

### Manual testing

```bash
make send-test-hook   # send a single PreToolUse event
make test             # full 3-phase test suite
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
│   ├── commands/
│   │   └── monitor-hooks.md      # /monitor-hooks slash command for Claude Code
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
├── lock_unix.go                 # Unix file locking (flock)
├── lock_windows.go              # Windows file locking (LockFileEx)
├── lock_common.go               # Shared lock diagnostics (showRunningInstance)
├── signals_unix.go              # Unix shutdown signals (SIGINT + SIGTERM)
├── signals_windows.go           # Windows shutdown signals (SIGINT only)
├── hooks/
│   ├── hook-client[.exe]        # Compiled Go hook client binary
│   ├── hook_monitor.py          # Alternative Python hook script
│   └── hook_monitor.conf        # Toggle: enable/disable hooks
├── plans/                       # Development planning documents
├── go.mod / go.sum              # Go dependencies (bubbletea, lipgloss, fatih/color)
├── Makefile                     # Build automation (run, run-ui, test, etc.)
├── install.sh                   # Bash installer (Linux/macOS)
├── install.ps1                  # PowerShell installer (Windows)
├── setup.sh                     # Ubuntu/Debian system deps installer
├── test-hooks.sh                # Test suite (3 phases)
├── README.md                    # This file
├── INSTALLME.md                 # Detailed installation guide
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

## Platform Notes

- **Windows TUI:** Bubble Tea supports Windows terminals (conhost, Windows Terminal) but is less tested than Linux/macOS.
- **Windows `run-background`:** The `make run-background` target is not available on Windows (uses `nohup`/`lsof`). Use `make run` in a separate terminal instead.
- **Windows hooks configuration:** On Windows, use backslash paths and `.exe` extension in `.claude/settings.json`:
  ```json
  "command": "C:\\Users\\you\\claude-hooks-monitor\\hooks\\hook-client.exe"
  ```
- **macOS `reset-config`:** `make reset-config` may not work on macOS due to `sed -i` syntax differences. Use `sed -i '' 's/= no/= yes/g' hooks/hook_monitor.conf` manually.

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
