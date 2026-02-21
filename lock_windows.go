//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// acquireLock tries to obtain an exclusive lock on a lock file using
// Windows LockFileEx. If another instance holds the lock, it reads the
// existing port file and prints info about the running instance, then exits.
func acquireLock(lockPath, portFilePath string) *os.File {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening lock file: %v\n", err)
		os.Exit(1)
	}

	// Try non-blocking exclusive lock.
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,        // reserved
		1,        // lock 1 byte
		0,        // high
		ol,
	)
	if err != nil {
		// Lock held by another process — read its info.
		f.Close()
		showRunningInstance(lockPath, portFilePath)
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
