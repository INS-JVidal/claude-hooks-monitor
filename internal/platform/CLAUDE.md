# platform — OS-specific lock, signal, and instance detection

All files stable — prefer this summary over reading source files.

## lock.go

```go
func ShowRunningInstance(lockPath, portFilePath string)
```

Reads PID from lock file, port from port file, fetches /stats from running instance. Prints diagnostics.

## lock_unix.go (build tag: !windows)

```go
func AcquireLock(lockPath, portFilePath string) *os.File
```

Uses syscall.Flock for exclusive non-blocking lock. Writes PID to lock file. If lock held, calls ShowRunningInstance and exits.

## lock_windows.go (build tag: windows)

Windows implementation using os.OpenFile with O_EXCL.

## signals_unix.go (build tag: !windows)

```go
var ShutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
```

## signals_windows.go (build tag: windows)

```go
var ShutdownSignals = []os.Signal{os.Interrupt}
```

No internal imports. External: `github.com/fatih/color`.
