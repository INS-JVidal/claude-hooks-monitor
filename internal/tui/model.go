package tui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"claude-hooks-monitor/internal/config"
	"claude-hooks-monitor/internal/hookevt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EventMsg wraps a hook event as a Bubble Tea message.
type EventMsg hookevt.HookEvent

// waitForEvent blocks on the event channel or context cancellation.
func waitForEvent(ctx context.Context, ch chan hookevt.HookEvent) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-ch:
			if !ok {
				return tea.Quit()
			}
			return EventMsg(event)
		case <-ctx.Done():
			return tea.Quit()
		}
	}
}

// Model is the Bubble Tea model for the hook event tree UI.
type Model struct {
	ctx       context.Context
	processor *EventProcessor
	sessions  []*Session
	rows      []FlatRow
	cursor    int
	eventCh   chan hookevt.HookEvent
	port      int
	width     int
	height    int
	ready     bool

	totalEvents int
	autoScroll  bool
	dropped     *atomic.Int64 // Shared counter — events dropped because channel was full

	version string // Build version displayed in header.

	// Detail pane state.
	detailOpen   bool
	detailLines  []string
	detailScroll int

	// Hooks menu state.
	hooksOpen     bool
	hooksReadOnly bool // True when config could not be read; blocks writes.
	hooksCfg      config.HookConfig
	hooksCursor   int
	hooksErr      string
	configPath    string
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, eventCh chan hookevt.HookEvent, port int, dropped *atomic.Int64, version, configPath string) Model {
	return Model{
		ctx:        ctx,
		processor:  NewEventProcessor(dropped),
		eventCh:    eventCh,
		port:       port,
		autoScroll: true,
		dropped:    dropped,
		version:    version,
		configPath: configPath,
	}
}

func (m Model) Init() tea.Cmd {
	return waitForEvent(m.ctx, m.eventCh)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case EventMsg:
		event := hookevt.HookEvent(msg)
		m.sessions = m.processor.Process(event)
		m.rows = FlattenTree(m.sessions)
		m.totalEvents++

		// Auto-scroll: keep cursor at bottom when user hasn't navigated away.
		if m.autoScroll && len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
		}

		// Refresh detail pane if open (e.g. PostPair arrived for a Pre event).
		if m.detailOpen && m.cursor >= 0 && m.cursor < len(m.rows) {
			m.detailLines = formatNodeDetail(m.rows[m.cursor].NodeRef, m.width)
			if m.detailScroll >= len(m.detailLines) {
				m.detailScroll = max(0, len(m.detailLines)-1)
			}
		}

		return m, waitForEvent(m.ctx, m.eventCh)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Hooks menu intercepts keys when open.
	if m.hooksOpen {
		return m.handleHooksKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "H":
		if !m.detailOpen {
			cfg, err := config.ReadConfig(m.configPath)
			if err != nil {
				m.hooksErr = err.Error()
				m.hooksReadOnly = true
			} else {
				m.hooksErr = ""
				m.hooksReadOnly = false
			}
			m.hooksCfg = cfg
			m.hooksCursor = 0
			m.hooksOpen = true
		}
		return m, nil

	case "i":
		if m.detailOpen {
			m.detailOpen = false
			m.detailLines = nil
			m.detailScroll = 0
			// Restore auto-scroll if cursor is at the bottom.
			if len(m.rows) > 0 && m.cursor == len(m.rows)-1 {
				m.autoScroll = true
			}
		} else if m.cursor >= 0 && m.cursor < len(m.rows) {
			m.detailOpen = true
			m.detailScroll = 0
			m.detailLines = formatNodeDetail(m.rows[m.cursor].NodeRef, m.width)
			m.autoScroll = false
		}
		return m, nil

	case "esc":
		if m.detailOpen {
			m.detailOpen = false
			m.detailLines = nil
			m.detailScroll = 0
			if len(m.rows) > 0 && m.cursor == len(m.rows)-1 {
				m.autoScroll = true
			}
			return m, nil
		}
		return m, nil

	case "up", "k":
		if m.detailOpen {
			if m.detailScroll > 0 {
				m.detailScroll--
			}
		} else {
			if m.cursor > 0 {
				m.cursor--
				m.autoScroll = false
			}
		}
		return m, nil

	case "down", "j":
		if m.detailOpen {
			m.detailScroll++
			// Clamp to content length to prevent unbounded drift.
			// renderDetailPane applies final clamping with the actual pane height.
			if m.detailScroll >= len(m.detailLines) {
				m.detailScroll = max(0, len(m.detailLines)-1)
			}
		} else {
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				// Re-enable auto-scroll if user reaches the bottom.
				if m.cursor == len(m.rows)-1 {
					m.autoScroll = true
				}
			}
		}
		return m, nil

	case "right", "l", "enter":
		if m.detailOpen {
			return m, nil
		}
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.HasChildren {
				setExpanded(row.NodeRef, true)
				m.rows = FlattenTree(m.sessions)
			}
		}
		return m, nil

	case "left", "h":
		if m.detailOpen {
			return m, nil
		}
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.HasChildren && row.Expanded {
				setExpanded(row.NodeRef, false)
				m.rows = FlattenTree(m.sessions)
			}
		}
		return m, nil

	case " ":
		if m.detailOpen {
			return m, nil
		}
		// Toggle expand/collapse.
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.HasChildren {
				setExpanded(row.NodeRef, !row.Expanded)
				m.rows = FlattenTree(m.sessions)
			}
		}
		return m, nil

	case "G":
		if m.detailOpen {
			return m, nil
		}
		// Jump to bottom, re-enable auto-scroll.
		if len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
			m.autoScroll = true
		}
		return m, nil

	case "g":
		if m.detailOpen {
			return m, nil
		}
		// Jump to top.
		m.cursor = 0
		m.autoScroll = false
		return m, nil
	}

	return m, nil
}

func (m Model) handleHooksKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "H", "esc":
		m.hooksOpen = false
		m.hooksErr = ""
		return m, nil

	case "up", "k":
		if m.hooksCursor > 0 {
			m.hooksCursor--
		}
		return m, nil

	case "down", "j":
		if m.hooksCursor < len(m.hooksCfg.Hooks)-1 {
			m.hooksCursor++
		}
		return m, nil

	case "enter", " ":
		if m.hooksReadOnly {
			return m, nil // Config could not be read; refuse writes to prevent data loss.
		}
		if m.hooksCursor >= 0 && m.hooksCursor < len(m.hooksCfg.Hooks) {
			m.hooksCfg.Hooks[m.hooksCursor].Enabled = !m.hooksCfg.Hooks[m.hooksCursor].Enabled
			if err := config.WriteConfig(m.configPath, m.hooksCfg); err != nil {
				m.hooksErr = "Write failed: " + err.Error()
			} else {
				m.hooksErr = ""
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Header.
	headerText := fmt.Sprintf(
		"Claude Hooks Monitor %s  │  Port %d  │  Events: %d",
		m.version, m.port, m.totalEvents,
	)
	if d := m.dropped.Load(); d > 0 {
		headerText += fmt.Sprintf("  │  Dropped: %d", d)
	}
	header := headerStyle.Render(headerText)

	// Footer — context-sensitive.
	var footerText string
	switch {
	case m.hooksOpen && m.hooksReadOnly:
		footerText = "esc/H: close  j/k: navigate  (read-only: config unreadable)"
	case m.hooksOpen:
		footerText = "esc/H: close  j/k: navigate  enter/space: toggle"
	case m.detailOpen:
		footerText = "esc/i: close  j/k: scroll detail"
	default:
		footerText = "q: quit  j/k: navigate  h/l: collapse/expand  space: toggle  g/G: top/bottom  i: detail  H: hooks"
	}
	footer := footerStyle.Render(footerText)

	// Available height for the tree viewport.
	viewHeight := m.height - 3 // header + footer + breathing room
	if viewHeight < 1 {
		viewHeight = 1
	}

	// Hooks menu replaces the tree area when open.
	if m.hooksOpen {
		body := renderHooksMenu(m.hooksCfg, m.hooksCursor, m.hooksErr, viewHeight, m.width)
		return header + "\n" + body + "\n" + footer
	}

	// Calculate detail pane height if open.
	paneHeight := 0
	if m.detailOpen && len(m.detailLines) > 0 {
		paneHeight = len(m.detailLines)
		halfAvail := viewHeight / 2
		if paneHeight > halfAvail {
			paneHeight = halfAvail
		}
		if paneHeight < 3 {
			paneHeight = 3
		}
		viewHeight = viewHeight - paneHeight - 1 // -1 for divider.
		if viewHeight < 1 {
			viewHeight = 1
		}
	}

	// Render visible tree rows with scrolling.
	var b strings.Builder

	if len(m.rows) == 0 {
		waiting := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true).
			Render("  Waiting for hook events...")
		b.WriteString(waiting)
		b.WriteByte('\n')
	} else {
		// Calculate visible window.
		start, end := visibleWindow(m.cursor, len(m.rows), viewHeight)

		for i := start; i < end; i++ {
			selected := i == m.cursor
			line := renderRow(m.rows[i], selected, m.width)
			b.WriteString(line)
			if i < end-1 {
				b.WriteByte('\n')
			}
		}
	}

	// Pad remaining lines to fill viewport (prevents flicker).
	lineCount := strings.Count(b.String(), "\n") + 1
	for i := lineCount; i < viewHeight; i++ {
		b.WriteByte('\n')
	}

	// Append detail pane if open.
	if m.detailOpen && paneHeight > 0 {
		divider := dividerStyle.Render(strings.Repeat("─", m.width))
		pane := renderDetailPane(m.detailLines, m.detailScroll, paneHeight, m.width)
		return header + "\n" + b.String() + "\n" + divider + "\n" + pane + "\n" + footer
	}

	return header + "\n" + b.String() + "\n" + footer
}

// visibleWindow calculates the start/end indices for the viewport window.
func visibleWindow(cursor, total, height int) (start, end int) {
	if total <= height {
		return 0, total
	}

	// Keep cursor roughly centered, clamped to bounds.
	half := height / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > total {
		end = total
		start = end - height
	}
	return start, end
}

// setExpanded toggles the Expanded field on the underlying tree node.
func setExpanded(nodeRef interface{}, expanded bool) {
	switch n := nodeRef.(type) {
	case *Session:
		n.Expanded = expanded
	case *UserRequest:
		n.Expanded = expanded
	case *EventNode:
		n.Expanded = expanded
	}
}

// Run starts the Bubble Tea TUI. Blocks until the user quits.
func Run(ctx context.Context, eventCh chan hookevt.HookEvent, port int, dropped *atomic.Int64, version, configPath string) error {
	p := tea.NewProgram(
		NewModel(ctx, eventCh, port, dropped, version, configPath),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}
