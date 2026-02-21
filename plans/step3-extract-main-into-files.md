# Step 3: Extract main.go into Focused Files

**Status:** Completed + Review fixes applied

## Goal

Purely mechanical moves — no logic changes. All stay `package main`.

## File Mapping

| New File | Content |
|----------|---------|
| `monitor.go` | `HookMonitor`, `NewHookMonitor`, `AddEvent`, `GetStats`, `GetEvents`, `logEvent`, `hookColor`, `hookColors`, `defaultColor`, `separator`, constants (`maxEvents`, `maxBodyLen`) |
| `server.go` | `handleHook`, `handleStats`, `handleEvents`, `handleHealth` |
| `lock.go` | `acquireLock`, `showRunningInstance` |
| `main.go` | Slim: imports, `main()` with flag parsing, hook registration, listener setup, startup, `printBanner` |

## Why This Works

All files in the same directory share a package namespace (`package main`), so splitting is purely organizational — no import changes needed between them. This is standard Go idiom for keeping files focused and navigable.

## Important: `go run`

After splitting, `go run main.go` no longer works (it only compiles one file). Must use `go run .` to compile the entire package. The Makefile `run` target was updated accordingly.

## Review Fixes Applied

1. **Safe lock path derivation** — `main.go` uses `strings.TrimSuffix(portFile, ".monitor-port")` instead of hardcoded slice arithmetic. Prevents panic if `PORT_FILE` doesn't end with `.monitor-port`.
2. **`showRunningInstance` accepts `lockPath` parameter** — Eliminates duplicated path derivation between `main.go` and `lock.go`. Single source of truth for the lock file path.

## Verification

- `go build -o bin/monitor .` succeeds
- `./bin/monitor` — behavior identical to before
- `./test-hooks.sh` — all integration tests pass (no Go unit tests exist yet)
- `PORT_FILE=custom_name ./bin/monitor` — no panic, derives lock path safely
