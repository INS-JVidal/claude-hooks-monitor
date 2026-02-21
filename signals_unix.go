//go:build !windows

package main

import (
	"os"
	"syscall"
)

// shutdownSignals lists OS signals that trigger graceful shutdown.
// On Unix, SIGTERM is included because `kill <PID>` sends it by default.
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
