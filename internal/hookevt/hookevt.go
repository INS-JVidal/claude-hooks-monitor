package hookevt

import "time"

// HookEvent represents a single hook event received from Claude Code.
type HookEvent struct {
	HookType  string                 `json:"hook_type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}
