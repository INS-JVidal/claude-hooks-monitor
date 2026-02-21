package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// hookTypeStyles maps hook types to lipgloss styles for TUI rendering.
var hookTypeStyles = map[string]lipgloss.Style{
	"SessionStart":       lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true),
	"SessionEnd":         lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
	"PreToolUse":         lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true),
	"PostToolUse":        lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true),
	"PostToolUseFailure": lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
	"UserPromptSubmit":   lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true),
	"Notification":       lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
	"PermissionRequest":  lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true),
	"Stop":               lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	"SubagentStart":      lipgloss.NewStyle().Foreground(lipgloss.Color("87")),
	"SubagentStop":       lipgloss.NewStyle().Foreground(lipgloss.Color("87")).Bold(true),
	"TeammateIdle":       lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
	"TaskCompleted":      lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
	"ConfigChange":       lipgloss.NewStyle().Foreground(lipgloss.Color("227")),
	"PreCompact":         lipgloss.NewStyle().Foreground(lipgloss.Color("207")),
}

var defaultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Bold(true)

	// Detail pane styles.
	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("237"))

	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255"))

	paneSectionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("239"))

	paneLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	paneValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
)

// hookStyle returns the lipgloss style for a given hook type.
func hookStyle(hookType string) lipgloss.Style {
	if s, ok := hookTypeStyles[hookType]; ok {
		return s
	}
	return defaultStyle
}

// renderRow produces a single rendered line for a FlatRow.
func renderRow(row FlatRow, selected bool, width int) string {
	indent := strings.Repeat("  ", row.Depth)

	icon := " "
	if row.HasChildren {
		if row.Expanded {
			icon = "▼"
		} else {
			icon = "▶"
		}
	}

	line := fmt.Sprintf("%s%s %s", indent, icon, row.Label)

	// Truncate to fit terminal width (rune-safe).
	if width > 0 && runewidth.StringWidth(line) > width-1 {
		line = runewidth.Truncate(line, width-1, "...")
	}

	style := hookStyle(row.HookType)
	if selected {
		// Compose: hook color + selected background.
		style = style.Background(lipgloss.Color("237"))
	}

	return style.Render(line)
}
