//go:build windows

package platform

import "os"

// ShutdownSignals lists OS signals that trigger graceful shutdown.
// On Windows, only os.Interrupt (Ctrl+C) is supported; SIGTERM does not exist.
var ShutdownSignals = []os.Signal{os.Interrupt}
