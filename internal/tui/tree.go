package tui

import (
	"time"

	"github.com/mattn/go-runewidth"
)

// Session groups all events from one Claude Code session.
type Session struct {
	ID        string
	StartTime time.Time
	Requests  []*UserRequest
	Expanded  bool
}

// UserRequest groups events between consecutive UserPromptSubmit hooks.
type UserRequest struct {
	Prompt    string
	Timestamp time.Time
	Events    []*EventNode
	Expanded  bool
}

// EventNode is a single hook event, possibly linked to its Post result.
type EventNode struct {
	HookType  string
	Timestamp time.Time
	ToolName  string
	Summary   string
	Data      map[string]interface{}
	PostPair  *EventNode // PreToolUse links to its PostToolUse/Failure
	Expanded  bool
	Evicted   bool // True if this Pre was evicted from pendingPre (Post will never arrive)
}

// FlatRow is one visible line in the viewport.
type FlatRow struct {
	Depth       int
	Label       string
	HookType    string
	NodeRef     interface{} // *Session, *UserRequest, or *EventNode
	HasChildren bool
	Expanded    bool
}

// FlattenTree walks expanded nodes and produces visible rows.
func FlattenTree(sessions []*Session) []FlatRow {
	var rows []FlatRow
	for _, s := range sessions {
		label := s.StartTime.Format("15:04:05") + " Session"
		if s.ID != "" {
			id := s.ID
			if len(id) > 12 {
				id = id[:12] + "…"
			}
			label += " [" + id + "]"
		}
		rows = append(rows, FlatRow{
			Depth:       0,
			Label:       label,
			HookType:    "SessionStart",
			NodeRef:     s,
			HasChildren: len(s.Requests) > 0,
			Expanded:    s.Expanded,
		})
		if !s.Expanded {
			continue
		}
		for _, req := range s.Requests {
			prompt := req.Prompt
			if runewidth.StringWidth(prompt) > 60 {
				prompt = runewidth.Truncate(prompt, 60, "...")
			}
			if prompt == "" {
				prompt = "(no prompt)"
			}
			rows = append(rows, FlatRow{
				Depth:       1,
				Label:       req.Timestamp.Format("15:04:05") + " " + prompt,
				HookType:    "UserPromptSubmit",
				NodeRef:     req,
				HasChildren: len(req.Events) > 0,
				Expanded:    req.Expanded,
			})
			if !req.Expanded {
				continue
			}
			for _, ev := range req.Events {
				label := ev.Timestamp.Format("15:04:05") + " " + ev.Summary
				if ev.Evicted {
					label += " (evicted)"
				}
				rows = append(rows, FlatRow{
					Depth:       2,
					Label:       label,
					HookType:    ev.HookType,
					NodeRef:     ev,
					HasChildren: ev.PostPair != nil,
					Expanded:    ev.Expanded,
				})
				if ev.Expanded && ev.PostPair != nil {
					post := ev.PostPair
					rows = append(rows, FlatRow{
						Depth:       3,
						Label:       post.Timestamp.Format("15:04:05") + " " + post.Summary,
						HookType:    post.HookType,
						NodeRef:     post,
						HasChildren: false,
						Expanded:    false,
					})
				}
			}
		}
	}
	return rows
}
