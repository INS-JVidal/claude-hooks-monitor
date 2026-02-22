package tui

import (
	"fmt"
	"sync/atomic"
	"time"

	"claude-hooks-monitor/internal/hookevt"

	"github.com/mattn/go-runewidth"
)

// maxPendingPerKey caps how many unmatched PreToolUse events are queued per
// pairKey. This prevents unbounded memory growth if Posts never arrive (e.g.
// tool calls that crash without triggering PostToolUse).
const maxPendingPerKey = 50

// EventProcessor groups incoming hook events into the tree data model.
type EventProcessor struct {
	sessions       []*Session
	sessionMap     map[string]*Session
	currentSession *Session
	currentRequest *UserRequest
	pendingPre     map[string][]*EventNode // pairKey → queue of unmatched Pre events (FIFO)
	dropped        *atomic.Int64           // shared counter for all event discard paths
}

// NewEventProcessor returns an initialized processor.
func NewEventProcessor(dropped *atomic.Int64) *EventProcessor {
	return &EventProcessor{
		sessionMap: make(map[string]*Session),
		pendingPre: make(map[string][]*EventNode),
		dropped:    dropped,
	}
}

// Process incorporates a new event into the tree and returns the updated sessions.
func (p *EventProcessor) Process(event hookevt.HookEvent) (sessions []*Session) {
	defer func() {
		if r := recover(); r != nil {
			// Don't crash TUI on malformed events — just skip.
			sessions = p.sessions
		}
	}()

	switch event.HookType {
	case "SessionStart":
		p.handleSessionStart(event)
	case "SessionEnd":
		p.handleSessionEnd(event)
	case "UserPromptSubmit":
		p.handleUserPrompt(event)
	default:
		p.handleGenericEvent(event)
	}

	return p.sessions
}

func (p *EventProcessor) handleSessionStart(event hookevt.HookEvent) {
	sid := strVal(event.Data, "session_id")

	// Only clear stale pending Pre entries when the previous session has
	// already ended (currentSession == nil). This avoids wiping unmatched
	// Pre events belonging to a concurrent session that is still active.
	if p.currentSession == nil {
		for k := range p.pendingPre {
			delete(p.pendingPre, k)
		}
	}

	// Check if session already exists (e.g., reconnect).
	if s, ok := p.sessionMap[sid]; ok {
		p.currentSession = s
		p.currentRequest = nil // Reset so events don't append to old request.
		return
	}

	s := &Session{
		ID:        sid,
		StartTime: event.Timestamp,
		Expanded:  true,
	}
	p.sessions = append(p.sessions, s)
	if sid != "" {
		p.sessionMap[sid] = s
	}
	p.currentSession = s
	p.currentRequest = nil
}

func (p *EventProcessor) handleSessionEnd(event hookevt.HookEvent) {
	// Just add an event node to the current request for visibility.
	node := &EventNode{
		HookType:  event.HookType,
		Timestamp: event.Timestamp,
		Summary:   "Session ended",
		Data:      event.Data,
	}
	p.appendToCurrentRequest(node, event.Timestamp)

	// Clear any unmatched Pre entries to avoid leaking memory across sessions.
	for k := range p.pendingPre {
		delete(p.pendingPre, k)
	}

	// Nil out session/request so post-end events don't append to the dead session.
	p.currentSession = nil
	p.currentRequest = nil
}

func (p *EventProcessor) handleUserPrompt(event hookevt.HookEvent) {
	p.ensureSession(event.Timestamp)

	prompt := strVal(event.Data, "prompt")
	req := &UserRequest{
		Prompt:    prompt,
		Timestamp: event.Timestamp,
		Expanded:  true,
	}
	p.currentSession.Requests = append(p.currentSession.Requests, req)
	p.currentRequest = req
}

func (p *EventProcessor) handleGenericEvent(event hookevt.HookEvent) {
	toolName := strVal(event.Data, "tool_name")
	summary := buildSummary(event)

	node := &EventNode{
		HookType:  event.HookType,
		Timestamp: event.Timestamp,
		ToolName:  toolName,
		Summary:   summary,
		Data:      event.Data,
	}

	// Use tool_use_id for Pre/Post pairing when available — this correctly
	// handles concurrent tool calls of the same type (e.g. two parallel Bash
	// calls). Falls back to tool_name when tool_use_id is absent.
	pairKey := strVal(event.Data, "tool_use_id")
	if pairKey == "" {
		pairKey = toolName
	}

	switch event.HookType {
	case "PreToolUse":
		queue := p.pendingPre[pairKey]
		// Cap per-key queue to prevent unbounded growth if Posts never arrive
		// (e.g. crashed tool calls). Oldest unmatched Pre is evicted and marked
		// so the TUI doesn't display it as permanently "pending".
		if len(queue) >= maxPendingPerKey {
			evicted := queue[0]
			evicted.Evicted = true
			queue = queue[1:]
			p.dropped.Add(1)
		}
		p.pendingPre[pairKey] = append(queue, node)
		p.appendToCurrentRequest(node, event.Timestamp)

	case "PostToolUse", "PostToolUseFailure":
		// Dequeue matching Pre (FIFO — oldest Pre pairs with first Post).
		if stack := p.pendingPre[pairKey]; len(stack) > 0 {
			pre := stack[0]
			if len(stack) == 1 {
				delete(p.pendingPre, pairKey) // clean up empty entry
			} else {
				p.pendingPre[pairKey] = stack[1:]
			}
			pre.PostPair = node
			// Don't add Post as a separate event — it's nested under Pre.
		} else if p.currentSession != nil {
			// Orphaned Post within an active session — add as standalone.
			p.appendToCurrentRequest(node, event.Timestamp)
		} else {
			// No active session — count as dropped rather than silently discarding.
			p.dropped.Add(1)
		}

	default:
		// Discard late-arriving events when no session is active to prevent
		// creating phantom "(default)" sessions after SessionEnd.
		if p.currentSession == nil {
			p.dropped.Add(1)
			return
		}
		p.appendToCurrentRequest(node, event.Timestamp)
	}
}

// ensureSession creates a default session if none exists yet.
func (p *EventProcessor) ensureSession(ts time.Time) {
	if p.currentSession == nil {
		s := &Session{
			ID:        "(default)",
			StartTime: ts,
			Expanded:  true,
		}
		p.sessions = append(p.sessions, s)
		p.currentSession = s
	}
}

// appendToCurrentRequest adds a node to the current request, creating one if needed.
func (p *EventProcessor) appendToCurrentRequest(node *EventNode, ts time.Time) {
	p.ensureSession(ts)
	if p.currentRequest == nil {
		req := &UserRequest{
			Prompt:    "(initial setup)",
			Timestamp: ts,
			Expanded:  true,
		}
		p.currentSession.Requests = append(p.currentSession.Requests, req)
		p.currentRequest = req
	}
	p.currentRequest.Events = append(p.currentRequest.Events, node)
}

// buildSummary generates a one-line display string for an event.
func buildSummary(event hookevt.HookEvent) string {
	toolName := strVal(event.Data, "tool_name")

	switch event.HookType {
	case "PreToolUse":
		input := inputSummary(event.Data)
		if input != "" {
			return fmt.Sprintf("%s: %s", toolName, input)
		}
		return toolName

	case "PostToolUse":
		return fmt.Sprintf("%s completed", toolName)

	case "PostToolUseFailure":
		return fmt.Sprintf("%s FAILED", toolName)

	case "UserPromptSubmit":
		prompt := strVal(event.Data, "prompt")
		if runewidth.StringWidth(prompt) > 60 {
			prompt = runewidth.Truncate(prompt, 60, "...")
		}
		return prompt

	case "SessionStart":
		return "Session started"

	case "SessionEnd":
		return "Session ended"

	case "Stop":
		return "Stop"

	case "Notification":
		msg := strVal(event.Data, "message")
		if runewidth.StringWidth(msg) > 50 {
			msg = runewidth.Truncate(msg, 50, "...")
		}
		if msg != "" {
			return "Notification: " + msg
		}
		return "Notification"

	case "SubagentStart":
		return "Subagent: " + strVal(event.Data, "agent_type")

	case "SubagentStop":
		return "Subagent stopped: " + strVal(event.Data, "agent_type")

	case "PermissionRequest":
		tool := strVal(event.Data, "tool_name")
		if tool != "" {
			return "Permission: " + tool
		}
		return "Permission requested"

	case "TeammateIdle":
		name := strVal(event.Data, "teammate_name")
		if name != "" {
			return "Idle: " + name
		}
		return "Teammate idle"

	case "TaskCompleted":
		task := strVal(event.Data, "task_name")
		if task != "" {
			return "Task done: " + task
		}
		return "Task completed"

	case "ConfigChange":
		key := strVal(event.Data, "key")
		if key != "" {
			return "Config: " + key
		}
		return "Config changed"

	case "PreCompact":
		return "Pre-compact"

	default:
		return event.HookType
	}
}

// inputSummary extracts a brief description from tool_input.
func inputSummary(data map[string]interface{}) string {
	input, ok := data["tool_input"]
	if !ok {
		return ""
	}
	m, ok := input.(map[string]interface{})
	if !ok {
		return ""
	}

	// Bash: show command
	if cmd, ok := m["command"]; ok {
		s := fmt.Sprintf("%v", cmd)
		if runewidth.StringWidth(s) > 50 {
			s = runewidth.Truncate(s, 50, "...")
		}
		return s
	}
	// Write/Read: show file path
	if fp, ok := m["file_path"]; ok {
		return fmt.Sprintf("%v", fp)
	}
	// Grep: show pattern
	if pat, ok := m["pattern"]; ok {
		return fmt.Sprintf("%v", pat)
	}

	return ""
}

// strVal safely extracts a string value from a map.
func strVal(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
