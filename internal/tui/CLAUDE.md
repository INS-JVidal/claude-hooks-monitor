# tui — Bubble Tea interactive tree UI for the monitor

All files stable — prefer this summary over reading source files.

## model.go

```go
type Model struct { /* unexported: ctx, processor, sessions, rows, cursor, eventCh, port, width, height, ready, totalEvents, autoScroll, dropped, version, detailOpen/Lines/Scroll, hooksOpen/ReadOnly/Cfg/Cursor/Err, configPath */ }
func NewModel(ctx context.Context, eventCh chan hookevt.HookEvent, port int, dropped *atomic.Int64, version, configPath string) Model
func Run(ctx context.Context, eventCh chan hookevt.HookEvent, port int, dropped *atomic.Int64, version, configPath string) error
```

Bubble Tea model: tree view with sessions → requests → events. Supports expand/collapse, vim keys, detail pane (i), hooks menu (H). waitForEvent blocks on channel or ctx.Done.

## tree.go

```go
type Session struct { ID string; StartTime time.Time; Requests []*UserRequest; Expanded bool }
type UserRequest struct { Prompt string; Timestamp time.Time; Events []*EventNode; Expanded bool }
type EventNode struct { HookType, ToolName, Summary string; Timestamp time.Time; Data map[string]interface{}; PostPair *EventNode; Expanded, Evicted bool }
type FlatRow struct { Depth int; Label, HookType string; NodeRef interface{}; HasChildren, Expanded bool }
func FlattenTree(sessions []*Session) []FlatRow
```

## processor.go

```go
type EventProcessor struct { /* unexported fields */ }
func NewEventProcessor(dropped *atomic.Int64) *EventProcessor
func (p *EventProcessor) Process(event hookevt.HookEvent) []*Session
```

Groups events into Session → UserRequest → EventNode tree. Pre/Post tool pairing via tool_use_id (FIFO queue, cap 50 per key). Session eviction at 50. Panic recovery on malformed events.

## detail.go

Renders detail panes for Session, UserRequest, EventNode. Shows tool_input fields in priority order, context fields, remaining data. Scrollable with word-wrapping (runewidth-safe). Unexported helpers: formatNodeDetail, renderDetailPane, wrapLines, sortStrings.

## hooks_menu.go

Renders hook toggle overlay using config.HookConfig. Checkbox UI with per-hook-type colors. Read-only mode when config is unreadable.

## styles.go

hookTypeStyles map (15 hook types → lipgloss colors). Styles: headerStyle, footerStyle, selectedStyle, dividerStyle, paneTitleStyle, paneSectionStyle, paneLabelStyle, paneValueStyle. `hookStyle(hookType string)`, `renderRow(row FlatRow, selected bool, width int) string`.

Imports: `hookevt` (HookEvent), `config` (HookConfig, ReadConfig, WriteConfig). External: `bubbletea`, `lipgloss`, `go-runewidth`.

## Keybindings

| Key | Action |
|-----|--------|
| q / ctrl+c | Quit |
| j / k (↑/↓) | Navigate |
| h / l | Collapse / expand node |
| space | Toggle expand |
| i | Open detail pane |
| H | Open hooks menu |
| g / G | Jump to top / bottom |
