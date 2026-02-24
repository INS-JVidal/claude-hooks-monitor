# Claude Code Hooks Monitor

A real-time monitoring system for [Claude Code CLI](https://docs.claude.com/en/docs/claude-code) hooks on **Linux Ubuntu**. It captures every hook event as it fires and displays them in a colorized terminal or an interactive tree UI.

> **Platform:** This project is developed and tested on Ubuntu (including WSL2). Other Linux distributions should work but are not officially supported.

## How It Works

The system has three parts that work together:

### 1. Hook Configuration (`hooks/hook_monitor.conf`)

A simple INI-style file that controls **which hooks are active**. Each of the 15 Claude Code hook types can be toggled on or off independently. The hook forwarder reads this file on every invocation, so changes take effect immediately without restarting anything.

```ini
[hooks]
SessionStart = yes
PreToolUse = yes
PostToolUse = no        # disable noisy tool hooks
Notification = yes
# ...
```

### 2. Hook Forwarders (`hooks/hook-client`)

A small Go binary that **Claude Code executes on every hook event**. Claude Code pipes a JSON payload into `hook-client` via stdin. The forwarder:

1. Reads the JSON payload from stdin (bounded to 1 MiB)
2. Checks `hook_monitor.conf` to see if this hook type is enabled
3. If enabled, POSTs the payload to the monitor's HTTP endpoint
4. Exits 0 immediately — it never blocks Claude Code

Claude Code's `.claude/settings.json` wires each hook type to this binary. The forwarder is intentionally minimal: a single-shot HTTP POST with a 2-second timeout, no keep-alive, no retries. If the monitor isn't running, the forwarder silently exits.

### 3. Monitor Client (`bin/monitor`)

The visualization server that **receives, stores, and displays hook events**. It runs in one of two modes:

- **Console mode** (`make run`): Colorized log output — each hook type gets a unique color, events print as they arrive.
- **Tree UI mode** (`make run-ui`): An interactive terminal UI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Events are organized into a collapsible hierarchy:

```
Session [a1b2c3d4e5f6]
 └─ Request "explain this function"
     ├─ PreToolUse: Read → /src/main.go
     │   └─ PostToolUse: Read completed
     ├─ PreToolUse: Bash → grep -rn "func main"
     │   └─ PostToolUse: Bash completed
     └─ Stop
```

The monitor also exposes a REST API (`/stats`, `/events`, `/health`) for programmatic access.

![Tree UI showing hook events organized by session, with a detail pane open on a SubagentStart event](images/monitor-hooks-ui.png)

## Prerequisites

- **Ubuntu** (20.04+ or WSL2)
- **Claude Code CLI** — latest version
- **Git** — required to clone the repository
- **Go** 1.24+ — only needed for source builds (precompiled binaries are downloaded by default)

## Installation

**One-line install** (downloads precompiled binaries):

```bash
curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

Custom install location:

```bash
INSTALL_DIR=~/projects/hooks-monitor \
  curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

Pin a specific version:

```bash
VERSION=v0.4.3 \
  curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

Force build from source (requires Go):

```bash
BUILD_FROM_SOURCE=1 \
  curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

**Manual install:**

```bash
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
make build
```

If you need Go and other system dependencies:

```bash
bash setup.sh
```

> For detailed step-by-step instructions, see [INSTALLME.md](INSTALLME.md).

## Quick Start

```bash
# Terminal 1: start the monitor
cd claude-hooks-monitor
make run            # console mode
# Or: make run-ui  # interactive tree UI

# Terminal 2: use Claude — hooks fire automatically
claude
```

## Hook Types

All 15 Claude Code hook events:

| # | Event | When it fires |
|---|-------|---------------|
| 1 | SessionStart | Session begins |
| 2 | UserPromptSubmit | User sends a prompt |
| 3 | PreToolUse | Before tool execution |
| 4 | PermissionRequest | Claude needs permission |
| 5 | PostToolUse | After tool succeeds |
| 6 | PostToolUseFailure | After tool fails |
| 7 | Notification | System notification |
| 8 | SubagentStart | Subagent launched |
| 9 | SubagentStop | Subagent completes |
| 10 | Stop | Claude stops responding |
| 11 | TeammateIdle | Teammate agent idle |
| 12 | TaskCompleted | Task completes |
| 13 | ConfigChange | Config changes |
| 14 | PreCompact | Before context compaction |
| 15 | SessionEnd | Session terminates |

## Configuring Hooks

### Within this project

No configuration needed — just `make run` in one terminal and `claude` in another. The included `.claude/settings.json` wires all 15 hook types to the forwarder automatically.

### In your own project

To monitor hooks while working in a different project:

1. Generate the hooks config with absolute paths:
   ```bash
   cd ~/claude-hooks-monitor
   make show-hooks-config
   ```
2. Copy the JSON output into your project's `.claude/settings.json`.
3. Start the monitor (`make run`) and then `claude` in your project.

### Toggle individual hooks

Edit `hooks/hook_monitor.conf` directly, or use the `/monitor-hooks` slash command from within Claude Code.

### `/monitor-hooks` slash command

A built-in Claude Code slash command for controlling hook monitoring without leaving your session. Available whenever Claude Code is running inside the monitor project directory.

**Subcommands:**

| Command | Effect |
|---------|--------|
| `/monitor-hooks help` | Show usage, valid hook types, and examples |
| `/monitor-hooks activate` | Enable all hooks |
| `/monitor-hooks deactivate` | Disable all hooks |
| `/monitor-hooks status` | Show monitor state + per-hook config |
| `/monitor-hooks show-all` | Audit: compare known hooks vs config (find missing/extra entries) |
| `/monitor-hooks <HookType> on` | Enable a specific hook |
| `/monitor-hooks <HookType> off` | Disable a specific hook |

**Examples:**

```
/monitor-hooks activate             # turn on all 15 hooks
/monitor-hooks PreToolUse off       # silence a noisy hook
/monitor-hooks status               # see what's on/off and if the monitor is running
/monitor-hooks show-all             # find hooks missing from config or unknown entries
```

Hook names are case-insensitive (e.g. `pretooluse` matches `PreToolUse`). Changes take effect immediately — the hook forwarder reads the config file on every invocation.

![/monitor-hooks activate command showing all 15 hooks enabled, with full output expanded via Ctrl+O](images/monitor-hooks-activate.png)

**Using from a different project:**

To install the `/monitor-hooks` command in another project:

```bash
make install-command PROJECT=~/my-project
```

This copies the slash command with absolute paths baked in, so it works regardless of which directory Claude Code is running in.

## Usage

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

### Tree UI key bindings

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `h` / `l` | Collapse/expand node |
| `Space` | Toggle expand/collapse |
| `i` | Open detail pane (full JSON) |
| `Esc` | Close detail pane |
| `H` | Open hook config toggle menu |
| `g` / `G` | Jump to top/bottom |
| `q` / `Ctrl+C` | Quit |

### API endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/hook/{HookType}` | Receive a hook event |
| GET | `/stats` | Aggregate hook counts |
| GET | `/events?limit=N` | Last N events (default 100) |
| GET | `/health` | Health check |

### Checking statistics

```bash
make stats          # aggregate hook counts
make check          # is the server running?
```

## Testing

```bash
make test           # full 3-phase test suite
make send-test-hook # send a single test event
```

The test suite has 3 phases:
1. **Direct server test** — sends all hook payloads directly to the server
2. **End-to-end test** — pipes JSON through the forwarder and verifies delivery
3. **Config toggle test** — disables a hook, verifies it's skipped, restores config

## Project Structure

```
claude-hooks-monitor/
├── cmd/
│   ├── monitor/main.go             # Monitor client — HTTP server, TUI, signal handling
│   └── hook-client/main.go         # Hook forwarder — reads stdin, POSTs to monitor
├── internal/
│   ├── config/config.go            # Shared hook config: AllHookTypes, INI read/write
│   ├── hookevt/hookevt.go          # Shared HookEvent type
│   ├── monitor/monitor.go          # Event buffer, stats, TUI channel
│   ├── server/server.go            # HTTP handlers (/hook, /stats, /events, /health)
│   ├── platform/                   # OS-specific locking and signal handling
│   └── tui/                        # Bubble Tea UI (model, tree, processor, styles)
├── hooks/
│   ├── hook-client                 # Compiled forwarder binary
│   └── hook_monitor.conf           # Hook toggle config
├── .claude/
│   ├── commands/monitor-hooks.md   # /monitor-hooks slash command
│   └── settings.json               # Hook wiring + permissions
├── Makefile                        # Build automation
├── install.sh                      # Installer script
├── setup.sh                        # Ubuntu system deps installer
├── test-hooks.sh                   # Test suite
├── INSTALLME.md                    # Detailed installation guide
├── EXAMPLES.md                     # Output examples
└── ARCHITECTURE.md                 # Architecture deep-dive
```

## Troubleshooting

**Server won't start:**
- Check port isn't in use: `lsof -i:8080`
- Try a different port: `PORT=9000 make run`

**Hooks don't fire:**
- Verify Claude sees them: run `/hooks` inside Claude Code
- Check settings.json: `cat .claude/settings.json | jq .`

**Hook fires but no event in monitor:**
- Is the monitor running? `make check`
- Is the hook enabled? `make show-config`
- Test manually: `echo '{"hook_event_name":"Test"}' | ./hooks/hook-client`

## Resources

- [Claude Code Hooks Documentation](https://docs.claude.com/en/docs/claude-code/hooks)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — TUI styling

## License

MIT
