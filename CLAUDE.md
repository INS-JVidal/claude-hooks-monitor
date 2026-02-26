# claude-hooks-monitor — Real-time Claude Code hook event monitor

Go module: `claude-hooks-monitor`. Receives hook events from hook-client, displays them in console or tree TUI, optionally forwards to hooks-store via EventSink.

**Single-instance lock:** Uses flock (Unix) / LockFileEx (Windows) via `platform.AcquireLock` to ensure only one monitor runs. If another instance is running, it prints the existing port and exits.

**hook-client is a separate binary** because Claude Code spawns it per-event and expects exit 0 immediately — it cannot be a long-running server.

**Fail-open hook toggles:** Missing config file or missing hook keys default to enabled. This ensures hooks are never silently dropped due to config issues.

## Build & Test

```bash
make build      # → bin/monitor + hooks/hook-client
make test       # go test ./...
make run        # console mode
make run-ui     # TUI mode (--ui flag)
```

## Key Files
- cmd/monitor/main.go — Entry point, two modes (console / TUI)
- cmd/hook-client/main.go — Per-event handler, reads stdin, POSTs to monitor
- internal/monitor/monitor.go — Core ring buffer, event routing, sink forwarding
- internal/server/server.go — HTTP handlers, auth middleware, deep clone
- internal/tui/model.go — Bubble Tea tree UI with detail pane and hooks menu
- internal/config/config.go — INI parsing, hook toggles, sink config

## Architecture
```
Claude Code → hook-client (stdin JSON) → POST /hook/<Type>
                                              ↓
                                    monitor.HookMonitor
                                    ├─ console: logEvent()
                                    ├─ TUI: eventCh → tui.Model
                                    └─ sink: HTTPSink → hooks-store
```

## Configuration
- Env: PORT, HOOK_MONITOR_TOKEN, HOOK_MONITOR_URL, PORT_FILE, HOOK_MONITOR_CONFIG
- Config: hook_monitor.conf ([hooks] toggles, [sink] forwarding)
- XDG: ~/.config/claude-hooks-monitor/
