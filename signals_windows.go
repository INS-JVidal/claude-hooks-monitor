//go:build windows

package main

import "os"

// shutdownSignals lists OS signals that trigger graceful shutdown.
// On Windows, only os.Interrupt (Ctrl+C) is supported; SIGTERM does not exist.
var shutdownSignals = []os.Signal{os.Interrupt}
