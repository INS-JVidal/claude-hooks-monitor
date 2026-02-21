# Step 1: Add Bubble Tea Dependencies

**Status:** Completed

## Goal

Add TUI framework dependencies to `go.mod`. No code changes.

## Commands

```bash
cd claude-hooks-monitor
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/charmbracelet/lipgloss@latest
go mod tidy
```

## Verification

- `go build .` succeeds with no code changes

## Notes

- `go mod tidy` upgraded Go version from 1.21 to 1.24.2 (required by bubbletea)
- `golang.org/x/sys` upgraded from v0.14.0 to v0.38.0
- Total new transitive dependencies: ~20 packages (ansi, termenv, cellbuf, etc.)
