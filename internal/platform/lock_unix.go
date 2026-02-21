//go:build !windows

package platform

import (
	"fmt"
	"os"
	"syscall"
)

// acquireLock tries to obtain an exclusive flock on a lock file.
// If another instance holds the lock, it reads the existing port file
// and prints info about the running instance, then exits.
func AcquireLock(lockPath, portFilePath string) *os.File {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		if os.IsPermission(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied opening lock file: %s\n", lockPath)
			os.Exit(2) // distinct from "already running" (exit 1)
		}
		fmt.Fprintf(os.Stderr, "Error opening lock file: %v\n", err)
		os.Exit(2)
	}

	// Try non-blocking exclusive lock.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Lock held by another process — read its info.
		f.Close()
		ShowRunningInstance(lockPath, portFilePath)
		os.Exit(1)
	}

	// Write our PID into the lock file for diagnostics.
	if err := f.Truncate(0); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: truncate lock file: %v\n", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: seek lock file: %v\n", err)
	}
	fmt.Fprintf(f, "%d", os.Getpid())
	f.Sync()

	return f // caller keeps this open to hold the lock
}
