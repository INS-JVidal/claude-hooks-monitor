# server — HTTP handlers and middleware for the monitor

All files stable — prefer this summary over reading source files.

## server.go

```go
func SecurityHeaders(next http.Handler) http.Handler
func AuthMiddleware(token string, next http.Handler) http.Handler
func HandleHook(mon *monitor.HookMonitor, hookType string) http.HandlerFunc
func HandleStats(mon *monitor.HookMonitor) http.HandlerFunc
func HandleEvents(mon *monitor.HookMonitor) http.HandlerFunc
func HandleCacheFile(mon *monitor.HookMonitor) http.HandlerFunc
func HandleHealth(w http.ResponseWriter, r *http.Request)
```

HandleHook: validates POST, reads body (bounded by MaxBodyLen), checks JSON depth (100 max), deep-clones data map, calls mon.AddEvent. HandleEvents: supports ?limit= query param (capped at MaxEvents). HandleCacheFile: GET /cache/file?session=X&path=Y, returns CacheQuery JSON from monitor's filecache (returns {found:false} if cache disabled). AuthMiddleware: constant-time bearer token comparison, /health exempt.

Unexported: cloneMap (recursive deep copy), cloneSlice, checkJSONDepth.

## server_test.go, integration_test.go

Unit tests: HandleHook success/errors, HandleStats, HandleEvents with limits, HandleHealth, SecurityHeaders, AuthMiddleware, cloneMap, checkJSONDepth. Integration tests: full server with auth, concurrent requests, event ordering.

Imports: `hookevt` (HookEvent), `monitor` (HookMonitor, MaxEvents, MaxBodyLen).
