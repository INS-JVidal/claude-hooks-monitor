# Step 6: Create `tui/` Package

**Status:** Completed + Review fixes applied

## Goal

Implement the full Bubble Tea interactive tree UI across four files.

## Files

### `tui/tree.go` — Data Model + Flattening

**Types:**
- `Session` — groups all events from one Claude Code session
- `UserRequest` — groups events between consecutive UserPromptSubmit hooks
- `EventNode` — single hook event, optionally linked to its Post result via `PostPair`
- `FlatRow` — one visible line in the viewport (depth, label, node reference)

**`FlattenTree(sessions)`** — recursive walk producing visible rows:
- Session (depth 0) → Request (depth 1) → Event (depth 2) → PostPair (depth 3)
- Only descend into children when `Expanded == true`
- This flat list lets the viewport treat the tree as a simple scrollable list
- Prompt truncation uses `runewidth.Truncate` for rune-safe display

### `tui/processor.go` — Event Processing + Tree Building

**`EventProcessor`** maintains:
- `sessions` — ordered list of sessions
- `sessionMap` — quick lookup by session ID
- `currentSession` / `currentRequest` — insertion points (niled on SessionEnd)
- `pendingPre` — `map[string][]*EventNode` — per-tool-name **queue** (FIFO) for Pre/Post pairing (cleared on SessionEnd)

**Pre/Post pairing** (queue-based, FIFO):
- `PreToolUse` → append to `pendingPre[toolName]`
- `PostToolUse`/`Failure` → dequeue first entry from `pendingPre[toolName]`, set `pre.PostPair = post`
- If queue empty → add Post as standalone event (orphaned)
- FIFO matches Claude Code's sequential tool execution: Pre₁ → Post₁ → Pre₂ → Post₂

**Design note:** The design document (`go_ui_planning.md`) describes a more complex Event interface hierarchy with `BaseEvent`, typed event structs, and `ToolUseID`-based pairing. The implementation simplifies this to a single `EventNode` struct with `map[string]interface{}` Data, paired by tool name. This is pragmatic: Claude Code hooks don't include a `ToolUseID`, and the flat data map provides maximum flexibility for future detail views

**Request grouping rules:**
1. `UserPromptSubmit` → start new request
2. `SessionStart` → create/update session; reset `currentRequest` on reconnect
3. `SessionEnd` → mark end, clear `pendingPre`, nil `currentSession`/`currentRequest`
4. All other events → append to current request
5. Events before first prompt → "(initial setup)" placeholder request
6. Events after SessionEnd without new SessionStart → create new default session

**`buildSummary(event)`** — one-line display (rune-safe truncation via `runewidth`):
- PreToolUse: `"Bash: echo hello"` or `"Write: /path/to/file"`
- PostToolUse: `"Bash completed"`
- PostToolUseFailure: `"Bash FAILED"`
- UserPromptSubmit: first 60 display-width chars of prompt
- Others: hook type name

**Panic safety:** `defer recover()` in `Process()` prevents malformed events from crashing the TUI.

### `tui/styles.go` — Lipgloss Styles + Row Renderer

- `hookTypeStyles` — maps all 15 hook types to lipgloss colors (matching the console mode colors)
- `headerStyle`, `footerStyle`, `selectedStyle` — UI chrome styles
- `renderRow(row, selected, width)` — produces a single styled line with indentation, expand/collapse icons (▶/▼), and rune-safe truncation via `runewidth.StringWidth`/`runewidth.Truncate`

### `tui/model.go` — Bubble Tea Model + Run

**`Model`** fields:
- `ctx` — for cancellation
- `processor` — event processor
- `sessions` / `rows` — current tree state
- `cursor` — selected row index
- `autoScroll` — tracks whether to follow new events
- `dropped` — `*atomic.Int64` shared with `HookMonitor` for dropped event visibility

**`waitForEvent(ctx, ch)`** — Bubble Tea command that:
- Blocks on event channel OR context cancellation
- Returns `EventMsg` or `tea.Quit`
- Prevents goroutine leak via context check

**Key bindings:**
| Key | Action |
|-----|--------|
| `q`, `ctrl+c` | Quit |
| `j`/`k`, `↑`/`↓` | Navigate up/down |
| `h`/`l`, `←`/`→` | Collapse/expand |
| `Space` | Toggle expand |
| `Enter` | Expand |
| `g` | Jump to top |
| `G` | Jump to bottom + re-enable auto-scroll |

**Auto-scroll behavior:**
- Enabled by default — cursor follows new events
- Disabled when user navigates away from bottom
- Re-enabled when user reaches bottom row or presses `G`

**Header display:**
- Shows port, total events received, and dropped event count (when > 0)

**`Run(ctx, eventCh, port, dropped)`** — entry point:
- Creates Bubble Tea program with `WithAltScreen()`
- Blocks until user quits
- Returns error for caller to handle

## Review Fixes Applied

1. **SessionEnd nils `currentSession`/`currentRequest`** — Events arriving after SessionEnd but before the next SessionStart now create a fresh default session instead of silently appending to the dead session.
2. **Rune-safe truncation everywhere** — All truncation sites (`buildSummary`, `inputSummary`, `FlattenTree`, `renderRow`) use `runewidth.StringWidth`/`runewidth.Truncate` instead of byte-count `len()`. Prevents invalid UTF-8 from slicing multi-byte runes (CJK, emoji, Unicode icons).
3. **Removed `WithMouseCellMotion()`** — Mouse events were being generated and silently ignored. Removed to eliminate unnecessary terminal escape sequence processing.
4. **Dropped event counter in header** — TUI header displays `"Dropped: N"` when the event channel overflows, making previously silent drops visible.

## Verification

1. `go build -o bin/monitor .` — compiles cleanly
2. `go vet ./...` — no warnings
3. `./bin/monitor` — existing behavior unchanged
4. `./bin/monitor --ui` — TUI starts in alt screen, shows header/footer
5. Send hooks via test script — events appear as tree nodes
6. Navigate with j/k, expand/collapse with arrows, quit with q — terminal restores cleanly
