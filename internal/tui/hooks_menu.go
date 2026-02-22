package tui

import (
	"fmt"
	"strings"

	"claude-hooks-monitor/internal/config"

	"github.com/charmbracelet/lipgloss"
)

var (
	hooksMenuTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("42"))

	hookEnabledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42")) // green

	hookDisabledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")) // dim

	hookErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")) // red
)

// renderHooksMenu renders the hooks configuration overlay.
func renderHooksMenu(cfg config.HookConfig, cursor int, errMsg string,
	viewHeight, width int) string {

	var b strings.Builder

	title := hooksMenuTitleStyle.Render("Hook Configuration")
	b.WriteString("  " + title + "\n")
	divLen := 40
	if width-4 < divLen {
		divLen = width - 4
	}
	if divLen < 10 {
		divLen = 10
	}
	b.WriteString("  " + dividerStyle.Render(strings.Repeat("─", divLen)) + "\n")

	for i, hook := range cfg.Hooks {
		checkbox := "[ ]"
		cbStyle := hookDisabledStyle
		if hook.Enabled {
			checkbox = "[x]"
			cbStyle = hookEnabledStyle
		}

		nameStyle := hookStyle(hook.Name)
		line := fmt.Sprintf("  %s %s", cbStyle.Render(checkbox), nameStyle.Render(hook.Name))

		if i == cursor {
			// Wrap the whole line with selected background.
			line = selectedStyle.Render(line)
		}

		b.WriteString(line)
		if i < len(cfg.Hooks)-1 {
			b.WriteByte('\n')
		}
	}

	if errMsg != "" {
		b.WriteString("\n\n")
		b.WriteString("  " + hookErrorStyle.Render("Error: "+errMsg))
	}

	// Pad remaining lines to fill viewport.
	lineCount := strings.Count(b.String(), "\n") + 1
	for i := lineCount; i < viewHeight; i++ {
		b.WriteByte('\n')
	}

	return b.String()
}
