package tui

import (
	"context"
	"fmt"
	"strings"

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
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, eventCh chan hookevt.HookEvent, port int) Model {
	return Model{
		ctx:        ctx,
		processor:  NewEventProcessor(),
		eventCh:    eventCh,
		port:       port,
		autoScroll: true,
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
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.autoScroll = false
		}
		return m, nil

	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			// Re-enable auto-scroll if user reaches the bottom.
			if m.cursor == len(m.rows)-1 {
				m.autoScroll = true
			}
		}
		return m, nil

	case "right", "l", "enter":
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.HasChildren {
				setExpanded(row.NodeRef, true)
				m.rows = FlattenTree(m.sessions)
			}
		}
		return m, nil

	case "left", "h":
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.HasChildren && row.Expanded {
				setExpanded(row.NodeRef, false)
				m.rows = FlattenTree(m.sessions)
			}
		}
		return m, nil

	case " ":
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
		// Jump to bottom, re-enable auto-scroll.
		if len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
			m.autoScroll = true
		}
		return m, nil

	case "g":
		// Jump to top.
		m.cursor = 0
		m.autoScroll = false
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Header.
	header := headerStyle.Render(fmt.Sprintf(
		"Claude Hooks Monitor  │  Port %d  │  Events: %d",
		m.port, m.totalEvents,
	))

	// Footer.
	footer := footerStyle.Render(
		"q: quit  j/k: navigate  h/l: collapse/expand  space: toggle  g/G: top/bottom",
	)

	// Available height for the tree viewport.
	viewHeight := m.height - 3 // header + footer + breathing room

	if viewHeight < 1 {
		viewHeight = 1
	}

	// Render visible rows with scrolling.
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
	lines := strings.Count(b.String(), "\n") + 1
	for i := lines; i < viewHeight; i++ {
		b.WriteByte('\n')
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
func Run(ctx context.Context, eventCh chan hookevt.HookEvent, port int) error {
	p := tea.NewProgram(
		NewModel(ctx, eventCh, port),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
