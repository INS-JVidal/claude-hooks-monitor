//go:build !windows

package platform

import (
	"os"
	"syscall"
)

// ShutdownSignals lists OS signals that trigger graceful shutdown.
// On Unix, SIGTERM is included because `kill <PID>` sends it by default.
var ShutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
