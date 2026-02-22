package tui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

const (
	labelWidth      = 12 // Column width for "Key:" alignment.
	maxContentLines = 10 // Limit for long content fields before truncation.
	maxMapKeys      = 50 // Cap rendered keys in nested maps to prevent memory spikes.
	maxTopLevelKeys = 100 // Cap rendered top-level data keys.
)

// formatNodeDetail returns human-readable lines for any tree node.
func formatNodeDetail(nodeRef interface{}, width int) []string {
	if width < 20 {
		width = 40 // Sane minimum for formatting.
	}
	wrapWidth := width - 4 // 2-space indent + margin.

	switch n := nodeRef.(type) {
	case *Session:
		return formatSession(n)
	case *UserRequest:
		return formatUserRequest(n, wrapWidth)
	case *EventNode:
		return formatEventNode(n, wrapWidth)
	}
	return []string{"(unknown node type)"}
}

func formatSession(s *Session) []string {
	var lines []string
	lines = append(lines, paneTitleStyle.Render("Session"))
	lines = append(lines, paneSectionStyle.Render(strings.Repeat("─", 35)))
	lines = append(lines, formatField("ID", s.ID))
	lines = append(lines, formatField("Started", s.StartTime.Format("15:04:05.000")))
	lines = append(lines, formatField("Requests", fmt.Sprintf("%d", len(s.Requests))))
	return lines
}

func formatUserRequest(r *UserRequest, wrapWidth int) []string {
	var lines []string
	lines = append(lines, paneTitleStyle.Render("User Request"))
	lines = append(lines, paneSectionStyle.Render(strings.Repeat("─", 35)))
	lines = append(lines, formatField("Time", r.Timestamp.Format("15:04:05.000")))
	lines = append(lines, formatField("Events", fmt.Sprintf("%d", len(r.Events))))

	if r.Prompt != "" {
		lines = append(lines, "")
		lines = append(lines, paneSectionStyle.Render("── Prompt "+strings.Repeat("─", 25)))
		for _, wl := range wrapLines(r.Prompt, wrapWidth) {
			lines = append(lines, "  "+paneValueStyle.Render(wl))
		}
	}
	return lines
}

func formatEventNode(ev *EventNode, wrapWidth int) []string {
	var lines []string

	// Title: "HookType · ToolName" or just "HookType".
	title := ev.HookType
	if ev.ToolName != "" {
		title += " · " + ev.ToolName
	}
	lines = append(lines, paneTitleStyle.Render(title))
	lines = append(lines, paneSectionStyle.Render(strings.Repeat("─", 35)))
	lines = append(lines, formatField("Time", ev.Timestamp.Format("15:04:05.000")))

	// Input section — show tool_input fields in deterministic order.
	if input, ok := ev.Data["tool_input"]; ok {
		if m, ok := input.(map[string]interface{}); ok {
			lines = append(lines, "")
			lines = append(lines, paneSectionStyle.Render("── Input "+strings.Repeat("─", 26)))
			lines = append(lines, formatInputFields(m, wrapWidth)...)
		}
	}

	// Context section — session_id, cwd, permission_mode, etc.
	contextLines := formatContextFields(ev.Data)
	if len(contextLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, paneSectionStyle.Render("── Context "+strings.Repeat("─", 24)))
		lines = append(lines, contextLines...)
	}

	// Data section — all remaining top-level fields not already shown.
	dataLines := formatRemainingData(ev.Data, wrapWidth)
	if len(dataLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, paneSectionStyle.Render("── Data "+strings.Repeat("─", 27)))
		lines = append(lines, dataLines...)
	}

	// Result section — PostPair if available.
	if ev.PostPair != nil {
		lines = append(lines, "")
		label := "Result (" + ev.PostPair.HookType + ")"
		lines = append(lines, paneSectionStyle.Render("── "+label+" "+strings.Repeat("─", max(1, 35-len(label)-4))))
		lines = append(lines, formatField("Status", ev.PostPair.Summary))
		lines = append(lines, formatField("Time", ev.PostPair.Timestamp.Format("15:04:05.000")))

		// Show tool_output if present.
		if output := strVal(ev.PostPair.Data, "tool_output"); output != "" {
			lines = append(lines, "")
			lines = append(lines, paneSectionStyle.Render("── Output "+strings.Repeat("─", 25)))
			lines = append(lines, formatLongValue(output, wrapWidth)...)
		}
	}

	return lines
}

// formatInputFields renders tool_input fields in a deterministic order.
func formatInputFields(input map[string]interface{}, wrapWidth int) []string {
	var lines []string

	// Priority fields — shown first in this order.
	priority := []struct{ key, label string }{
		{"command", "Command"},
		{"file_path", "File"},
		{"pattern", "Pattern"},
		{"query", "Query"},
		{"content", "Content"},
		{"new_string", "New string"},
		{"old_string", "Old string"},
		{"description", "Description"},
		{"url", "URL"},
	}

	for _, p := range priority {
		if v, ok := input[p.key]; ok {
			s := fmt.Sprintf("%v", v)
			if p.key == "content" || p.key == "new_string" || p.key == "old_string" {
				lines = append(lines, paneLabelStyle.Render(pad(p.label+":", labelWidth))+" ")
				lines = append(lines, formatLongValue(s, wrapWidth)...)
			} else {
				wrapped := wrapLines(s, wrapWidth-labelWidth-1)
				for i, wl := range wrapped {
					if i >= maxContentLines {
						lines = append(lines, strings.Repeat(" ", labelWidth+2)+paneSectionStyle.Render(fmt.Sprintf("... (%d more lines)", len(wrapped)-i)))
						break
					}
					if i == 0 {
						lines = append(lines, formatField(p.label, wl))
					} else {
						lines = append(lines, strings.Repeat(" ", labelWidth+2)+paneValueStyle.Render(wl))
					}
				}
			}
		}
	}

	// Remaining fields — alphabetical, skip internal/noisy ones.
	skip := map[string]bool{"command": true, "file_path": true, "pattern": true,
		"query": true, "content": true, "new_string": true, "old_string": true,
		"description": true, "url": true}
	var remaining []string
	for k := range input {
		if !skip[k] {
			remaining = append(remaining, k)
		}
	}
	// Sort for deterministic output.
	sortStrings(remaining)
	for _, k := range remaining {
		s := fmt.Sprintf("%v", input[k])
		if strings.Contains(s, "\n") || len(s) > wrapWidth {
			lines = append(lines, paneLabelStyle.Render(pad(k+":", labelWidth))+" ")
			lines = append(lines, formatLongValue(s, wrapWidth)...)
		} else {
			lines = append(lines, formatField(k, s))
		}
	}

	return lines
}

// formatContextFields extracts context-level fields from the event data.
func formatContextFields(data map[string]interface{}) []string {
	var lines []string
	contextKeys := []struct{ key, label string }{
		{"session_id", "Session"},
		{"cwd", "Directory"},
		{"permission_mode", "Permission"},
		{"agent_type", "Agent"},
		{"message", "Message"},
	}
	for _, ck := range contextKeys {
		if v := strVal(data, ck.key); v != "" {
			lines = append(lines, formatField(ck.label, v))
		}
	}
	return lines
}

// formatRemainingData renders all top-level keys in ev.Data that weren't shown
// by the Input or Context sections.
func formatRemainingData(data map[string]interface{}, wrapWidth int) []string {
	// Keys already handled by other sections.
	shown := map[string]bool{
		"tool_input":      true,
		"tool_output":     true,
		"hook_event_name": true, // Redundant with the title.
		"session_id":      true,
		"cwd":             true,
		"permission_mode": true,
		"agent_type":      true,
		"message":         true,
		"tool_name":       true, // Already in the title.
	}

	var keys []string
	for k := range data {
		if !shown[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sortStrings(keys)

	var lines []string
	for i, k := range keys {
		if i >= maxTopLevelKeys {
			lines = append(lines, paneSectionStyle.Render(fmt.Sprintf("... (%d more keys)", len(keys)-i)))
			break
		}
		lines = append(lines, formatValue(k, data[k], wrapWidth)...)
	}
	return lines
}

// formatValue renders a single key-value pair, handling maps, slices, and scalars.
func formatValue(key string, val interface{}, wrapWidth int) []string {
	switch v := val.(type) {
	case map[string]interface{}:
		var lines []string
		lines = append(lines, paneLabelStyle.Render(pad(key+":", labelWidth)))
		var subKeys []string
		for sk := range v {
			subKeys = append(subKeys, sk)
		}
		sortStrings(subKeys)
		for i, sk := range subKeys {
			if i >= maxMapKeys {
				lines = append(lines, "  "+paneSectionStyle.Render(fmt.Sprintf("... (%d more keys)", len(subKeys)-i)))
				break
			}
			s := fmt.Sprintf("%v", v[sk])
			if strings.Contains(s, "\n") || len(s) > wrapWidth {
				lines = append(lines, "  "+paneLabelStyle.Render(pad(sk+":", labelWidth))+" ")
				lines = append(lines, formatLongValue(s, wrapWidth)...)
			} else {
				lines = append(lines, "  "+formatField(sk, s))
			}
		}
		return lines

	case []interface{}:
		var lines []string
		lines = append(lines, paneLabelStyle.Render(pad(key+":", labelWidth)))
		for i, item := range v {
			s := fmt.Sprintf("%v", item)
			prefix := fmt.Sprintf("  [%d] ", i)
			if strings.Contains(s, "\n") || len(s) > wrapWidth {
				lines = append(lines, prefix)
				lines = append(lines, formatLongValue(s, wrapWidth)...)
			} else {
				lines = append(lines, prefix+paneValueStyle.Render(s))
			}
			if i >= maxContentLines-1 && i < len(v)-1 {
				lines = append(lines, "  "+paneSectionStyle.Render(fmt.Sprintf("... (%d more items)", len(v)-i-1)))
				break
			}
		}
		return lines

	default:
		s := fmt.Sprintf("%v", val)
		if strings.Contains(s, "\n") || len(s) > wrapWidth {
			var lines []string
			lines = append(lines, paneLabelStyle.Render(pad(key+":", labelWidth))+" ")
			lines = append(lines, formatLongValue(s, wrapWidth)...)
			return lines
		}
		return []string{formatField(key, s)}
	}
}

// formatLongValue renders a potentially long string, truncating after maxContentLines.
func formatLongValue(s string, wrapWidth int) []string {
	wrapped := wrapLines(s, wrapWidth)
	if len(wrapped) <= maxContentLines {
		var lines []string
		for _, wl := range wrapped {
			lines = append(lines, "  "+paneValueStyle.Render(wl))
		}
		return lines
	}
	var lines []string
	for _, wl := range wrapped[:maxContentLines] {
		lines = append(lines, "  "+paneValueStyle.Render(wl))
	}
	remaining := len(wrapped) - maxContentLines
	lines = append(lines, "  "+paneSectionStyle.Render(fmt.Sprintf("... (%d more lines)", remaining)))
	return lines
}

// formatField returns "  Label:     value" with aligned columns.
func formatField(label, value string) string {
	return paneLabelStyle.Render(pad(label+":", labelWidth)) + " " + paneValueStyle.Render(value)
}

// pad right-pads a string to width with spaces.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// wrapLines splits s into lines of at most maxWidth display columns,
// preserving existing newlines and breaking on spaces.
// Uses runewidth.StringWidth for correct handling of multi-byte characters
// (CJK, emoji) that occupy more than one terminal column.
func wrapLines(s string, maxWidth int) []string {
	if maxWidth < 10 {
		maxWidth = 10
	}
	var result []string
	for _, paragraph := range strings.Split(s, "\n") {
		if paragraph == "" {
			result = append(result, "")
			continue
		}
		for len(paragraph) > 0 {
			if runewidth.StringWidth(paragraph) <= maxWidth {
				result = append(result, paragraph)
				break
			}
			// Find last space within maxWidth display columns.
			cut := runewidth.Truncate(paragraph, maxWidth, "")
			cutLen := len(cut)
			if idx := strings.LastIndex(cut, " "); idx > 0 {
				cutLen = idx
			}
			// Guarantee forward progress: if Truncate returned "" (first rune
			// wider than maxWidth, e.g. CJK in a very narrow terminal), consume
			// at least one rune to prevent an infinite loop.
			if cutLen == 0 {
				r := []rune(paragraph)
				cutLen = len(string(r[:1]))
			}
			result = append(result, paragraph[:cutLen])
			paragraph = strings.TrimLeft(paragraph[cutLen:], " ")
		}
	}
	if len(result) == 0 {
		result = []string{""}
	}
	return result
}

// renderDetailPane renders the scrollable detail pane.
func renderDetailPane(lines []string, scrollOffset, visHeight, width int) string {
	if len(lines) == 0 {
		return ""
	}

	// Clamp scroll offset.
	maxScroll := len(lines) - visHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}

	// Direct start/end — scrollOffset is a top-of-window offset, not a cursor.
	start := scrollOffset
	end := start + visHeight
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString("  ")
		b.WriteString(lines[i])
		if i < end-1 {
			b.WriteByte('\n')
		}
	}

	// Scroll indicator when content exceeds pane height.
	if len(lines) > visHeight {
		indicator := paneSectionStyle.Render(fmt.Sprintf(" [%d/%d]", scrollOffset+1, len(lines)))
		b.WriteString("  ")
		b.WriteString(indicator)
	}

	return b.String()
}

// sortStrings sorts a slice of strings in place (simple insertion sort — tiny slices).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
