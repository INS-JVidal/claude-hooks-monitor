# Step 5: Add `--ui` Flag with Coordinated Shutdown

**Status:** Completed + Review fixes applied

## Goal

Add `--ui` flag that switches between console mode and TUI mode, with proper coordinated shutdown in both cases.

## Changes to `main.go`

```go
func main() {
    uiMode := flag.Bool("ui", false, "Start interactive tree UI")
    flag.Parse()

    // ... lock/port setup ...

    lockFd := acquireLock(lockFile, portFile)
    os.Remove(portFile) // Remove stale port file from previous crash

    // ... create monitor, mux, listener, write port file ...

    ctx, cancel := context.WithCancel(context.Background())
    server := &http.Server{Handler: mux}

    // Unified cleanup — deferred so it runs on both normal exit and signal.
    defer func() {
        cancel()
        server.Shutdown(context.Background())
        os.Remove(portFile)
        lockFd.Close()
        os.Remove(lockFile)
    }()

    // Unified signal handler for both modes.
    go func() {
        sig := make(chan os.Signal, 1)
        signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
        <-sig
        cancel()
        server.Shutdown(context.Background())
    }()

    if *uiMode {
        go server.Serve(ln)
        tui.Run(ctx, eventCh, actualPort, &monitor.Dropped)
    } else {
        printBanner(actualPort, len(hookTypes))
        server.Serve(ln) // Unblocked by server.Shutdown from signal handler
    }
    // Both paths fall through → deferred cleanup runs
}
```

## Robustness Points

1. **No `os.Exit` anywhere** — both modes fall through to deferred cleanup, so defers always run and terminal is always restored
2. **Unified signal handler** — single goroutine handles SIGINT/SIGTERM for both modes. Cancels context (signals TUI) and calls `server.Shutdown()` (unblocks `server.Serve` in console mode)
3. **Unified deferred cleanup** — single cleanup function for both modes. No duplicated cleanup logic between signal handler and TUI-mode defer
4. **`context.Context`** flows to TUI for cancellation signaling
5. **`http.Server`** used instead of raw `http.Serve` to support `Shutdown()`
6. **Dedicated `http.ServeMux`** instead of `DefaultServeMux` — cleaner, avoids global state

## Review Fixes Applied

1. **Unified shutdown** — Eliminated separate `setupSignalHandler` function (which used `os.Exit(0)` and bypassed defers). Both modes now share the same signal handler and deferred cleanup.
2. **Stale port file removal** — After acquiring the lock (which proves we're the only instance), `os.Remove(portFile)` cleans up any stale port file from a previous crash. This closes the window where a crashed process leaves a port file pointing to a dead port.

## Verification

- `./bin/monitor` — console mode, ctrl+c exits cleanly (defers run, files cleaned up)
- `./bin/monitor --ui` — starts TUI, exits cleanly with `q` or ctrl+c
- `./bin/monitor --help` — shows `--ui` flag
- Kill with `kill -15 <pid>` — graceful shutdown in both modes
