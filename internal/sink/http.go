package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"claude-hooks-monitor/internal/hookevt"
)

// HTTPSink forwards events via HTTP POST to an external endpoint.
// It is safe for concurrent use. Errors are returned but never block
// the caller — the monitor fires Send in a background goroutine.
type HTTPSink struct {
	endpoint string
	client   *http.Client
}

// NewHTTPSink creates an HTTPSink that POSTs events to the given endpoint.
// The client uses a short timeout to avoid blocking the monitor if the
// companion is slow or unreachable.
func NewHTTPSink(endpoint string) *HTTPSink {
	return &HTTPSink{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     60 * time.Second,
				DisableKeepAlives:   false,
				MaxIdleConnsPerHost: 5,
			},
		},
	}
}

// Send marshals the event to JSON and POSTs it to the configured endpoint.
// Returns an error on marshal failure, network error, or non-2xx response.
func (s *HTTPSink) Send(ctx context.Context, event hookevt.HookEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("sink: marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sink: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sink: post to %s: %w", s.endpoint, err)
	}
	// Drain and close the body so the HTTP transport can reuse the connection
	// for keep-alive. Without draining, the transport must close the connection.
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sink: unexpected status %d from %s", resp.StatusCode, s.endpoint)
	}
	return nil
}

// Close releases idle connections held by the HTTP transport.
func (s *HTTPSink) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
