package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-hooks-monitor/internal/monitor"
	"claude-hooks-monitor/internal/server"
)

// buildFullStack creates an httptest.Server with the full middleware chain
// (SecurityHeaders + optional Auth) and all routes registered, matching
// the production wiring in cmd/monitor/main.go.
func buildFullStack(t *testing.T, token string) (*httptest.Server, *monitor.HookMonitor) {
	t.Helper()
	mon := monitor.NewHookMonitor(nil)

	mux := http.NewServeMux()

	hookTypes := []string{
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest",
		"PostToolUse", "PostToolUseFailure", "Notification", "SubagentStart",
		"SubagentStop", "Stop", "TeammateIdle", "TaskCompleted",
		"ConfigChange", "PreCompact", "SessionEnd",
	}

	hookLookup := make(map[string]string, len(hookTypes))
	for _, ht := range hookTypes {
		mux.HandleFunc("/hook/"+ht, server.HandleHook(mon, ht))
		hookLookup[strings.ToLower(ht)] = ht
	}
	mux.HandleFunc("/hook/", func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/hook/")
		if canonical, ok := hookLookup[strings.ToLower(raw)]; ok {
			server.HandleHook(mon, canonical)(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"unknown hook type"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/stats", server.HandleStats(mon))
	mux.HandleFunc("/events", server.HandleEvents(mon))
	mux.HandleFunc("/health", server.HandleHealth)

	var handler http.Handler = mux
	handler = server.SecurityHeaders(handler)
	if token != "" {
		handler = server.AuthMiddleware(token, handler)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, mon
}

func postJSON(t *testing.T, url, token string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func getJSON(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func respJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return m
}

// ============================================================
// End-to-End Flows
// ============================================================

func TestIntegration_FullLifecycle(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	// POST 3 events
	for _, hook := range []string{"PreToolUse", "SessionStart", "PreToolUse"} {
		resp := postJSON(t, srv.URL+"/hook/"+hook, "", []byte(`{"data":"test"}`))
		if resp.StatusCode != 200 {
			t.Fatalf("POST /hook/%s: status %d", hook, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// GET /stats
	resp := getJSON(t, srv.URL+"/stats", "")
	m := respJSON(t, resp)
	if m["total_hooks"] != float64(3) {
		t.Errorf("total_hooks = %v, want 3", m["total_hooks"])
	}

	// GET /events
	resp = getJSON(t, srv.URL+"/events", "")
	m = respJSON(t, resp)
	if m["count"] != float64(3) {
		t.Errorf("count = %v, want 3", m["count"])
	}

	// GET /health
	resp = getJSON(t, srv.URL+"/health", "")
	m = respJSON(t, resp)
	if m["status"] != "healthy" {
		t.Errorf("health status = %v, want healthy", m["status"])
	}
}

func TestIntegration_CaseInsensitiveHookRouting(t *testing.T) {
	t.Parallel()
	srv, mon := buildFullStack(t, "")

	// POST to lowercase URL path
	resp := postJSON(t, srv.URL+"/hook/pretooluse", "", []byte(`{}`))
	if resp.StatusCode != 200 {
		t.Fatalf("POST /hook/pretooluse: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Stats should count under the canonical "PreToolUse"
	stats := mon.GetStats()
	if stats["PreToolUse"] != 1 {
		t.Errorf("stats[PreToolUse] = %d, want 1; full stats: %v", stats["PreToolUse"], stats)
	}
}

func TestIntegration_UnknownHookType(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	resp := postJSON(t, srv.URL+"/hook/NonExistent", "", []byte(`{}`))
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestIntegration_EventOrdering(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	names := []string{"SessionStart", "PreToolUse", "PostToolUse"}
	for _, name := range names {
		resp := postJSON(t, srv.URL+"/hook/"+name, "", []byte(fmt.Sprintf(`{"name":"%s"}`, name)))
		resp.Body.Close()
	}

	resp := getJSON(t, srv.URL+"/events?limit=3", "")
	m := respJSON(t, resp)
	events := m["events"].([]interface{})
	for i, name := range names {
		evt := events[i].(map[string]interface{})
		if evt["hook_type"] != name {
			t.Errorf("event[%d].hook_type = %v, want %s", i, evt["hook_type"], name)
		}
	}
}

func TestIntegration_LimitPagination(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	for i := 0; i < 50; i++ {
		resp := postJSON(t, srv.URL+"/hook/PreToolUse", "", []byte(fmt.Sprintf(`{"i":%d}`, i)))
		resp.Body.Close()
	}

	resp := getJSON(t, srv.URL+"/events?limit=10", "")
	m := respJSON(t, resp)
	if m["count"] != float64(10) {
		t.Errorf("count = %v, want 10", m["count"])
	}
	// Should be the LAST 10 events (i=40..49)
	events := m["events"].([]interface{})
	first := events[0].(map[string]interface{})
	data := first["data"].(map[string]interface{})
	if data["i"] != float64(40) {
		t.Errorf("first event i = %v, want 40", data["i"])
	}
}

// ============================================================
// Security Integration (Attack Simulation)
// ============================================================

func TestSecurity_AuthRequired(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "my-secret-token")

	// Without token, all endpoints except /health should 401
	endpoints := []struct {
		method string
		path   string
	}{
		{"POST", "/hook/PreToolUse"},
		{"GET", "/stats"},
		{"GET", "/events"},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			var resp *http.Response
			if ep.method == "POST" {
				resp = postJSON(t, srv.URL+ep.path, "", []byte(`{}`))
			} else {
				resp = getJSON(t, srv.URL+ep.path, "")
			}
			defer resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Errorf("expected 401 without token, got %d", resp.StatusCode)
			}
		})
	}

	// With token, should work
	resp := postJSON(t, srv.URL+"/hook/PreToolUse", "my-secret-token", []byte(`{}`))
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 with token, got %d", resp.StatusCode)
	}
}

func TestSecurity_AuthTokenInQueryParam(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "my-secret-token")

	// Token in query param should NOT work
	resp := getJSON(t, srv.URL+"/stats?token=my-secret-token", "")
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for token in query param, got %d", resp.StatusCode)
	}
}

func TestSecurity_PathTraversalViaURL(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	paths := []string{
		"/hook/../../etc/passwd",
		"/hook/../../../etc/shadow",
		"/hook/..%2f..%2fetc%2fpasswd",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp := postJSON(t, srv.URL+path, "", []byte(`{}`))
			defer resp.Body.Close()
			// Should get 404 or redirect, NOT access to files
			if resp.StatusCode == 200 {
				body, _ := io.ReadAll(resp.Body)
				// Verify it's a hook response, not file content
				var m map[string]interface{}
				if json.Unmarshal(body, &m) != nil || m["status"] == nil {
					t.Errorf("unexpected 200 for path traversal: %s", body)
				}
			}
		})
	}
}

func TestSecurity_SQLInjectionPayload(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	payload := `{"name":"'; DROP TABLE hooks; --","id":"1 OR 1=1"}`
	resp := postJSON(t, srv.URL+"/hook/PreToolUse", "", []byte(payload))
	defer resp.Body.Close()
	// Should store it as data (no SQL)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for SQL injection payload, got %d", resp.StatusCode)
	}
}

func TestSecurity_XSSPayload(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	payload := `{"html":"<script>alert(1)</script>","img":"<img src=x onerror=alert(1)>"}`
	resp := postJSON(t, srv.URL+"/hook/PreToolUse", "", []byte(payload))
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Verify nosniff header prevents type sniffing
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff on XSS payload response")
	}
}

func TestSecurity_HugePayload(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	// 2 MiB — exceeds the 1 MiB limit, will be truncated → invalid JSON
	body := `{"x":"` + strings.Repeat("A", 2<<20) + `"}`
	resp := postJSON(t, srv.URL+"/hook/PreToolUse", "", []byte(body))
	defer resp.Body.Close()
	// Truncated JSON is invalid → 400
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for oversized payload, got %d", resp.StatusCode)
	}
}

func TestSecurity_SlowLoris(t *testing.T) {
	t.Parallel()
	// Build a server with real timeouts (httptest.Server doesn't set these by default,
	// so we build our own)
	mon := monitor.NewHookMonitor(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/hook/Test", server.HandleHook(mon, "Test"))
	handler := server.SecurityHeaders(mux)

	s := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 1 * time.Second, // short for test
		ReadTimeout:       2 * time.Second,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(ln)
	t.Cleanup(func() { s.Close() })

	addr := ln.Addr().String()

	// Open raw TCP connection and send headers very slowly
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Send partial headers
	conn.Write([]byte("POST /hook/Test HTTP/1.1\r\nHost: localhost\r\n"))
	// Wait for server to timeout
	time.Sleep(2 * time.Second)

	// Try to send more — should fail or get an error
	_, err = conn.Write([]byte("Content-Length: 2\r\n\r\n{}"))
	if err == nil {
		// Connection might still be open; try to read — should be closed/error
		buf := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, readErr := conn.Read(buf)
		if readErr == nil && n > 0 {
			response := string(buf[:n])
			// If we get a response, it should be a timeout/error
			if strings.Contains(response, "200 OK") {
				t.Error("slowloris attack succeeded — server should have timed out")
			}
		}
	}
}

func TestSecurity_ManyConnections(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	const n = 100
	var wg sync.WaitGroup
	errors := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := postJSON(t, srv.URL+"/hook/PreToolUse", "", []byte(fmt.Sprintf(`{"i":%d}`, i)))
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				errors <- fmt.Errorf("request %d: status %d", i, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestSecurity_JSONBomb(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	// Deeply nested JSON (depth 200) should be rejected
	var b bytes.Buffer
	for i := 0; i < 200; i++ {
		b.WriteString(`{"a":`)
	}
	b.WriteString(`1`)
	for i := 0; i < 200; i++ {
		b.WriteString(`}`)
	}

	resp := postJSON(t, srv.URL+"/hook/PreToolUse", "", b.Bytes())
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for JSON bomb, got %d", resp.StatusCode)
	}
}

func TestSecurity_MethodOverrideHeader(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/hook/PreToolUse", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HTTP-Method-Override", "DELETE")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should still process as POST, not DELETE
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 (ignore method override), got %d", resp.StatusCode)
	}
}

func TestSecurity_ContentTypeManipulation(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/hook/PreToolUse", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "text/xml") // Wrong content type
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Handler ignores Content-Type, just parses JSON
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 (ignores content type), got %d", resp.StatusCode)
	}
}

func TestSecurity_ResponseHeadersPresent(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "")

	endpoints := []struct {
		method string
		path   string
		body   []byte
	}{
		{"GET", "/health", nil},
		{"GET", "/stats", nil},
		{"GET", "/events", nil},
		{"POST", "/hook/PreToolUse", []byte(`{}`)},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			var resp *http.Response
			if ep.method == "POST" {
				resp = postJSON(t, srv.URL+ep.path, "", ep.body)
			} else {
				resp = getJSON(t, srv.URL+ep.path, "")
			}
			defer resp.Body.Close()

			if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing X-Content-Type-Options: nosniff")
			}
			if resp.Header.Get("Cache-Control") != "no-store" {
				t.Error("missing Cache-Control: no-store")
			}
		})
	}
}

func TestSecurity_HealthExemptFromAuth(t *testing.T) {
	t.Parallel()
	srv, _ := buildFullStack(t, "super-secret")

	// Health should work without any auth
	resp := getJSON(t, srv.URL+"/health", "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for /health without auth, got %d", resp.StatusCode)
	}
}
