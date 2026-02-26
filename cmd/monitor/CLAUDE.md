# cmd/monitor — Entry point for the claude-hooks-monitor binary

All files stable — prefer this summary over reading source files.

## main.go

CLI flags: --ui (enables Bubble Tea TUI mode). Two modes: console (blocking Serve) or TUI (go Serve + tui.Run blocks).

Wiring: XDG config discovery → single-instance lock → HookMonitor (with optional eventCh) → sink config → file cache config → HTTP mux with case-insensitive hook routing + /cache/file endpoint → SecurityHeaders + optional AuthMiddleware → signal handler → coordinated shutdown via sync.Once.

Port file written atomically for hook-client discovery. Config file discovery: env var → XDG dir → fallback dir.

`var version = "dev"` — set by ldflags at build time.

Imports: `config`, `filecache`, `hookevt`, `monitor`, `platform`, `server`, `sink`, `tui`.
