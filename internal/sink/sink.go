package sink

import (
	"context"

	"claude-hooks-monitor/internal/hookevt"
)

// EventSink is the outbound port for forwarding events to external consumers.
// Implementations must be safe for concurrent use from multiple goroutines.
type EventSink interface {
	// Send forwards an event to the external consumer.
	// Implementations should respect ctx cancellation and deadlines.
	Send(ctx context.Context, event hookevt.HookEvent) error

	// Close releases any resources held by the sink.
	Close() error
}
