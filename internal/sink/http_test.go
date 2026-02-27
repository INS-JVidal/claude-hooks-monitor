package sink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"claude-hooks-monitor/internal/hookevt"
)

func testEvent() hookevt.HookEvent {
	return hookevt.HookEvent{
		HookType:  "PreToolUse",
		Timestamp: time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC),
		Data: map[string]interface{}{
			"tool_name": "Write",
			"session_id": "test-session",
		},
	}
}

func TestHTTPSink_SendSuccess(t *testing.T) {
	t.Parallel()

	var received hookevt.HookEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	s := NewHTTPSink(srv.URL)
	defer s.Close()

	err := s.Send(context.Background(), testEvent())
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	if received.HookType != "PreToolUse" {
		t.Errorf("received HookType = %q, want PreToolUse", received.HookType)
	}
	if received.Data["tool_name"] != "Write" {
		t.Errorf("received tool_name = %v, want Write", received.Data["tool_name"])
	}
}

func TestHTTPSink_SendNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewHTTPSink(srv.URL)
	defer s.Close()

	err := s.Send(context.Background(), testEvent())
	if err == nil {
		t.Fatal("Send() should return error on 500")
	}
}

func TestHTTPSink_SendConnectionRefused(t *testing.T) {
	t.Parallel()

	// Connect to a port that's not listening.
	s := NewHTTPSink("http://127.0.0.1:1")
	defer s.Close()

	err := s.Send(context.Background(), testEvent())
	if err == nil {
		t.Fatal("Send() should return error on connection refused")
	}
}

func TestHTTPSink_SendContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // slow server
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewHTTPSink(srv.URL)
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := s.Send(ctx, testEvent())
	if err == nil {
		t.Fatal("Send() should return error when context expires")
	}
}

func TestHTTPSink_ConcurrentSend(t *testing.T) {
	t.Parallel()

	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	s := NewHTTPSink(srv.URL)
	defer s.Close()

	const n = 50
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			errs <- s.Send(context.Background(), testEvent())
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Send() error: %v", err)
		}
	}

	if got := count.Load(); got != n {
		t.Errorf("server received %d requests, want %d", got, n)
	}
}

func TestHTTPSink_Close(t *testing.T) {
	t.Parallel()

	s := NewHTTPSink("http://localhost:9999")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}
