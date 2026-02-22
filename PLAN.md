# Comprehensive Testing Plan — Claude Hooks Monitor

## Executive Summary

Current state: **1 of 8 packages has tests** (`internal/config` — 11 tests). Only happy-path coverage exists via a shell script (`test-hooks.sh`). No CI. This plan adds **~180 test cases** across 5 new test files plus a GitHub Actions CI workflow, organized into four tiers: unit tests, integration tests, security/adversarial tests, and concurrency stress tests.

---

## Architecture of Changes

```
NEW FILES:
├── .github/workflows/test.yml                    # CI workflow
├── internal/server/server_test.go                 # ~65 tests
├── internal/monitor/monitor_test.go               # ~35 tests
├── internal/config/config_edge_test.go            # ~20 tests (extends existing)
├── cmd/hook-client/client_test.go                 # ~40 tests
└── internal/server/integration_test.go            # ~20 tests (full-stack E2E)
```

---

## Phase 1: Server Unit Tests (Highest Value)

**File:** `internal/server/server_test.go`

The HTTP server is the primary attack surface. Every handler, middleware, and validator needs thorough coverage.

### 1A. HandleHook — Happy Path
| Test | Description |
|------|-------------|
| `TestHandleHook_ValidJSON` | POST valid JSON body → 200 OK, response has `{"status":"ok","hook":"PreToolUse"}` |
| `TestHandleHook_EmptyBody` | POST with empty body → 200 OK, event stored with empty `Data` map |
| `TestHandleHook_EventStored` | Verify `mon.GetEvents(1)` contains the posted event with correct HookType |
| `TestHandleHook_ResponseContentType` | Check `Content-Type: application/json` header in response |

### 1B. HandleHook — Error Paths
| Test | Description |
|------|-------------|
| `TestHandleHook_WrongMethod_GET` | GET request → 405 Method Not Allowed |
| `TestHandleHook_WrongMethod_PUT` | PUT request → 405 |
| `TestHandleHook_WrongMethod_DELETE` | DELETE → 405 |
| `TestHandleHook_InvalidJSON` | POST `{broken` → 400, `{"error":"invalid JSON"}` |
| `TestHandleHook_JSONArray` | POST `[1,2,3]` → 400 (expects object, not array) |
| `TestHandleHook_JSONString` | POST `"hello"` → 400 |
| `TestHandleHook_JSONNumber` | POST `42` → 400 |

### 1C. HandleHook — Security & Edge Cases
| Test | Description |
|------|-------------|
| `TestHandleHook_JSONDepthExceeded` | Construct JSON nested 101 levels → 400, error mentions "depth" |
| `TestHandleHook_JSONDepthExact` | Construct JSON nested exactly 100 levels → 200 OK (boundary) |
| `TestHandleHook_JSONDepth99` | Nested 99 levels → 200 OK |
| `TestHandleHook_BodySizeLimit` | POST body of exactly 1 MiB → 200 OK |
| `TestHandleHook_BodyOverLimit` | POST body of 1 MiB + 1 byte → verify truncation (server reads only 1 MiB) |
| `TestHandleHook_BinaryPayload` | POST raw binary (non-JSON) → 400 invalid JSON |
| `TestHandleHook_NullBytesInJSON` | POST JSON with embedded `\u0000` → 200 OK (valid JSON) |
| `TestHandleHook_UnicodePayload` | POST JSON with CJK/emoji/RTL strings → 200 OK, data preserved |
| `TestHandleHook_DuplicateKeys` | POST `{"a":1,"a":2}` → 200 OK, last wins (Go JSON behavior) |
| `TestHandleHook_DeepClone` | Post nested JSON, mutate original map, verify stored event is independent |
| `TestHandleHook_LargeNumberOfKeys` | Post JSON with 10,000 keys → 200 OK (no key-count limit, body within 1 MiB) |

### 1D. HandleStats
| Test | Description |
|------|-------------|
| `TestHandleStats_Empty` | No events → `{"stats":{},"total_hooks":0,"dropped":0}` |
| `TestHandleStats_AfterEvents` | Add 3 events of 2 types → stats counts correct |
| `TestHandleStats_WrongMethod` | POST → 405 |
| `TestHandleStats_DroppedCount` | Fill TUI channel, verify dropped counter in response |

### 1E. HandleEvents
| Test | Description |
|------|-------------|
| `TestHandleEvents_Empty` | No events → `{"events":[],"count":0,"dropped":0}` |
| `TestHandleEvents_DefaultLimit` | Add 150 events → GET /events returns last 100 (default limit) |
| `TestHandleEvents_CustomLimit` | GET /events?limit=5 → returns exactly 5 |
| `TestHandleEvents_LimitZero` | GET /events?limit=0 → returns all (treated as "no limit") |
| `TestHandleEvents_LimitNegative` | GET /events?limit=-1 → returns all |
| `TestHandleEvents_LimitOverMax` | GET /events?limit=9999 → capped at MaxEvents (1000) |
| `TestHandleEvents_LimitNotANumber` | GET /events?limit=abc → defaults to 100 |
| `TestHandleEvents_LimitFloat` | GET /events?limit=3.14 → defaults to 100 |
| `TestHandleEvents_WrongMethod` | POST → 405 |

### 1F. HandleHealth
| Test | Description |
|------|-------------|
| `TestHandleHealth_OK` | GET → 200 with `{"status":"healthy","time":"..."}` |
| `TestHandleHealth_WrongMethod` | POST → 405 |
| `TestHandleHealth_ResponseFormat` | Verify time is valid RFC3339 |

### 1G. SecurityHeaders Middleware
| Test | Description |
|------|-------------|
| `TestSecurityHeaders_XContentType` | Response has `X-Content-Type-Options: nosniff` |
| `TestSecurityHeaders_CacheControl` | Response has `Cache-Control: no-store` |

### 1H. AuthMiddleware
| Test | Description |
|------|-------------|
| `TestAuth_ValidToken` | Correct `Bearer <token>` → 200 |
| `TestAuth_WrongToken` | Wrong token → 401 |
| `TestAuth_MissingHeader` | No Authorization header → 401 |
| `TestAuth_EmptyToken` | `Authorization: Bearer ` → 401 |
| `TestAuth_NoBearer` | `Authorization: <token>` (missing "Bearer ") → 401 |
| `TestAuth_BearerCaseSensitive` | `Authorization: bearer <token>` (lowercase) → 401 |
| `TestAuth_HealthBypass` | GET /health without token → 200 (exempt) |
| `TestAuth_HealthWithToken` | GET /health with valid token → 200 |
| `TestAuth_TimingAttack` | Send many requests with different-length tokens, verify response times are constant (statistical, informational) |

### 1I. checkJSONDepth helper
| Test | Description |
|------|-------------|
| `TestCheckJSONDepth_FlatObject` | `{"a":1}` → nil |
| `TestCheckJSONDepth_EmptyObject` | `{}` → nil |
| `TestCheckJSONDepth_EmptyArray` | `[]` → nil (note: this still fails at Unmarshal as not a map) |
| `TestCheckJSONDepth_MaxDepthObject` | 100 nested objects → nil |
| `TestCheckJSONDepth_OverMaxObject` | 101 nested objects → error |
| `TestCheckJSONDepth_MixedDepth` | Mix of arrays and objects to depth 100 → nil |
| `TestCheckJSONDepth_MixedOverMax` | Mix of arrays/objects to depth 101 → error |
| `TestCheckJSONDepth_InvalidJSON` | Broken JSON → nil (defers to Unmarshal) |

### 1J. cloneMap / cloneSlice helpers
| Test | Description |
|------|-------------|
| `TestCloneMap_Nil` | nil input → nil output |
| `TestCloneMap_Shallow` | `{"a":"b"}` → independent copy |
| `TestCloneMap_DeepNested` | 3-level nested map → independent copy, mutation doesn't propagate |
| `TestCloneMap_WithSlice` | Map containing slice → slice independently copied |
| `TestCloneSlice_Empty` | Empty slice → empty but non-nil |
| `TestCloneSlice_NestedMaps` | Slice of maps → each map independently copied |

---

## Phase 2: Monitor Unit Tests

**File:** `internal/monitor/monitor_test.go`

### 2A. Ring Buffer Behavior
| Test | Description |
|------|-------------|
| `TestAddEvent_Basic` | Add 1 event, GetEvents returns it |
| `TestAddEvent_OrderPreserved` | Add 10 events, GetEvents returns in insertion order |
| `TestAddEvent_MaxCapacity` | Add exactly 1000 events, all present |
| `TestAddEvent_TrimAt1001` | Add 1001 events, only last 900 present (trimmed to trimTarget) |
| `TestAddEvent_TrimMultiple` | Add 2000 events, verify exactly last 900 after final trim |
| `TestAddEvent_TrimPreservesNewest` | Add 1001, verify event #102..#1001 are present, #1..#101 are gone |
| `TestGetEvents_LimitZero` | GetEvents(0) → returns all |
| `TestGetEvents_LimitNegative` | GetEvents(-1) → returns all |
| `TestGetEvents_LimitExceedsCount` | GetEvents(500) with 10 events → returns 10 |
| `TestGetEvents_ReturnsIndependentSlice` | Modify returned slice → original unaffected |

### 2B. Stats
| Test | Description |
|------|-------------|
| `TestGetStats_Empty` | No events → empty map |
| `TestGetStats_Counting` | Add 5 PreToolUse + 3 SessionStart → correct counts |
| `TestGetStats_ReturnsIndependentMap` | Modify returned map → original unaffected |

### 2C. TUI Channel Management
| Test | Description |
|------|-------------|
| `TestChannel_EventForwarded` | Add event with TUI channel → event received on channel |
| `TestChannel_DroppedWhenFull` | Fill 256-event channel, add one more → Dropped counter increments |
| `TestChannel_NilChannel` | nil channel → no panic, event still stored in buffer |
| `TestCloseChannel_Idempotent` | Call CloseChannel() twice → no panic |
| `TestCloseChannel_DropsAfterClose` | CloseChannel(), then AddEvent → Dropped increments, no panic |
| `TestCloseChannel_NilChannel` | NewHookMonitor(nil), CloseChannel() → no panic |

### 2D. Concurrency (run with `-race`)
| Test | Description |
|------|-------------|
| `TestConcurrent_AddEvent` | 100 goroutines each add 100 events → no race, stats consistent |
| `TestConcurrent_AddAndRead` | Writers add events while readers call GetEvents/GetStats → no race |
| `TestConcurrent_AddAndClose` | Writers add events while CloseChannel is called → no panic, no race |
| `TestConcurrent_StressDropped` | 100 goroutines send events with full TUI channel → Dropped count = total dropped |

---

## Phase 3: Hook Client Tests

**File:** `cmd/hook-client/client_test.go`

Since the hook client is `package main`, we test its exported helper functions by extracting them via test-accessible patterns. The functions `isAlphaOnly`, `truncate`, `getEnv`, `getEnvInt`, `isHookEnabled`, and `discoverMonitorURL` are all in `package main` and testable from `client_test.go` in the same package.

### 3A. isAlphaOnly — Input Validation (Security-Critical)
| Test | Description |
|------|-------------|
| `TestIsAlphaOnly_ValidLower` | `"pretooluse"` → true |
| `TestIsAlphaOnly_ValidUpper` | `"PRETOOLUSE"` → true |
| `TestIsAlphaOnly_ValidMixed` | `"PreToolUse"` → true |
| `TestIsAlphaOnly_Empty` | `""` → false |
| `TestIsAlphaOnly_Numbers` | `"hook123"` → false |
| `TestIsAlphaOnly_PathTraversal` | `"../../etc/passwd"` → false |
| `TestIsAlphaOnly_URLEncoded` | `"%2e%2e"` → false |
| `TestIsAlphaOnly_SlashInject` | `"hook/../../admin"` → false |
| `TestIsAlphaOnly_SpaceInject` | `"hook type"` → false |
| `TestIsAlphaOnly_Newline` | `"hook\ntype"` → false |
| `TestIsAlphaOnly_NullByte` | `"hook\x00type"` → false |
| `TestIsAlphaOnly_Unicode` | `"hóok"` → false (non-ASCII) |
| `TestIsAlphaOnly_SingleChar` | `"a"` → true |
| `TestIsAlphaOnly_Semicolon` | `"hook;rm -rf /"` → false |
| `TestIsAlphaOnly_Backtick` | `` "hook`id`" `` → false |

### 3B. truncate — UTF-8 Safety
| Test | Description |
|------|-------------|
| `TestTruncate_ShortString` | `"abc"` at 10 → `"abc"` (no-op) |
| `TestTruncate_ExactLength` | `"abc"` at 3 → `"abc"` |
| `TestTruncate_ASCIICut` | `"abcde"` at 3 → `"abc"` |
| `TestTruncate_MultiByteBoundary` | `"café"` at 4 → `"caf"` (doesn't split the é) |
| `TestTruncate_CJKBoundary` | `"日本語"` at 4 → `"日"` (3-byte char, 4 doesn't split next) |
| `TestTruncate_EmojiBoundary` | `"hello😀world"` at 6 → `"hello"` (doesn't split 4-byte emoji) |
| `TestTruncate_EmptyString` | `""` at 5 → `""` |
| `TestTruncate_ZeroMaxLen` | `"abc"` at 0 → `""` |

### 3C. discoverMonitorURL — URL Validation (Security-Critical)
| Test | Description |
|------|-------------|
| `TestDiscoverURL_EnvVarLocalhost` | `HOOK_MONITOR_URL=http://localhost:8080` → accepted |
| `TestDiscoverURL_EnvVar127` | `HOOK_MONITOR_URL=http://127.0.0.1:8080` → accepted |
| `TestDiscoverURL_EnvVarIPv6` | `HOOK_MONITOR_URL=http://[::1]:8080` → accepted |
| `TestDiscoverURL_EnvVarHTTPS` | `HOOK_MONITOR_URL=https://localhost:8080` → rejected (empty) |
| `TestDiscoverURL_EnvVarRemoteHost` | `HOOK_MONITOR_URL=http://evil.com:8080` → rejected |
| `TestDiscoverURL_EnvVarNoScheme` | `HOOK_MONITOR_URL=localhost:8080` → rejected (scheme != "http") |
| `TestDiscoverURL_EnvVarFTP` | `HOOK_MONITOR_URL=ftp://localhost:8080` → rejected |
| `TestDiscoverURL_PortFileValid` | Port file contains `8080` → `http://localhost:8080` |
| `TestDiscoverURL_PortFileInvalid` | Port file contains `abc` → empty |
| `TestDiscoverURL_PortFileZero` | Port file contains `0` → empty (port < 1) |
| `TestDiscoverURL_PortFile65536` | Port file contains `65536` → empty (port > 65535) |
| `TestDiscoverURL_PortFileMissing` | No port file → empty |
| `TestDiscoverURL_PortFileNegative` | Port file contains `-1` → empty |
| `TestDiscoverURL_SSRFAttempt` | `HOOK_MONITOR_URL=http://169.254.169.254/latest/meta-data` → rejected (not loopback) |
| `TestDiscoverURL_DNSRebind` | `HOOK_MONITOR_URL=http://attacker.com:8080` → rejected |

### 3D. isHookEnabled — Config Parsing
| Test | Description |
|------|-------------|
| `TestIsHookEnabled_MissingFile` | Nonexistent file → true (fail-open) |
| `TestIsHookEnabled_EmptyFile` | Empty file → true |
| `TestIsHookEnabled_Disabled` | `[hooks]\nPreToolUse = no` → false |
| `TestIsHookEnabled_Enabled` | `[hooks]\nPreToolUse = yes` → true |
| `TestIsHookEnabled_BOM` | BOM prefix + `[hooks]\nPreToolUse = no` → false |
| `TestIsHookEnabled_CaseInsensitive` | `[hooks]\npretooluse = no` → false for "PreToolUse" |
| `TestIsHookEnabled_InlineComment` | `[hooks]\nPreToolUse = no # disabled` → false |
| `TestIsHookEnabled_WrongSection` | `[other]\nPreToolUse = no` → true (not in [hooks]) |
| `TestIsHookEnabled_LastWins` | `[hooks]\nPreToolUse = yes\nPreToolUse = no` → false |
| `TestIsHookEnabled_EmptyValue` | `[hooks]\nPreToolUse = ` → true (empty != "no") |

### 3E. getEnvInt — Environment Parsing
| Test | Description |
|------|-------------|
| `TestGetEnvInt_Default` | Unset env → default |
| `TestGetEnvInt_Valid` | `"5"` → 5 |
| `TestGetEnvInt_NonNumeric` | `"abc"` → default |
| `TestGetEnvInt_Zero` | `"0"` → default (n <= 0 returns default) |
| `TestGetEnvInt_Negative` | `"-1"` → default |

---

## Phase 4: Config Edge Cases (Extends Existing)

**File:** `internal/config/config_edge_test.go`

These extend the existing 11 tests with edge cases and security scenarios.

### 4A. AtomicWriteFile
| Test | Description |
|------|-------------|
| `TestAtomicWrite_CreatesFile` | Write to new path → file exists with correct content |
| `TestAtomicWrite_OverwritesExisting` | Write twice → second content wins |
| `TestAtomicWrite_Permissions` | Written with 0600 → verified via os.Stat |
| `TestAtomicWrite_NoTempLeftover` | After successful write → no `.tmp.*` files remain |
| `TestAtomicWrite_InvalidDir` | Write to `/nonexistent/dir/file` → error, no temp left |
| `TestAtomicWrite_ConcurrentReads` | Writer + 100 concurrent readers → readers see old or new, never partial |

### 4B. ReadConfig / WriteConfig Edge Cases
| Test | Description |
|------|-------------|
| `TestReadConfig_EmptyFile` | Empty file → all hooks enabled |
| `TestReadConfig_NoHooksSection` | File with `[other]` only → all enabled |
| `TestReadConfig_MultipleSections` | `[hooks]` then `[other]` then `[hooks]` → second [hooks] overrides |
| `TestReadConfig_WindowsLineEndings` | `\r\n` → parses correctly |
| `TestReadConfig_TrailingWhitespace` | Keys/values with extra spaces → parsed correctly |
| `TestReadConfig_NoEqualsSign` | Line without `=` → skipped |
| `TestReadConfig_ValueWithEquals` | `key = a=b` → value is `a=b` |
| `TestReadConfig_VeryLongLine` | 100KB line → no crash |
| `TestWriteConfig_ReadableOutput` | Written file contains human-readable header comments |

### 4C. parseINIHooks Adversarial Inputs
| Test | Description |
|------|-------------|
| `TestParseINI_MalformedSection` | `[hooks` (no closing bracket) → treated as key, skipped |
| `TestParseINI_EmptySection` | `[]` → not [hooks], ignored |
| `TestParseINI_OnlyComments` | File of only `#` lines → all enabled |
| `TestParseINI_BinaryGarbage` | Random bytes → gracefully returns all enabled |

---

## Phase 5: Full-Stack Integration Tests (User Perspective)

**File:** `internal/server/integration_test.go`

These simulate the full user journey: start a server, send hook events via HTTP, verify state via the stats/events APIs.

### 5A. End-to-End Flows
| Test | Description |
|------|-------------|
| `TestIntegration_FullLifecycle` | Start server → POST 3 events → GET /stats verifies counts → GET /events verifies data → GET /health is healthy |
| `TestIntegration_CaseInsensitiveHookRouting` | POST to `/hook/pretooluse` (lowercase) → server routes to canonical `PreToolUse`, stats count under `PreToolUse` |
| `TestIntegration_UnknownHookType` | POST to `/hook/NonExistent` → 404 `{"error":"unknown hook type"}` |
| `TestIntegration_EventOrdering` | POST events A, B, C → GET /events returns [A, B, C] in order |
| `TestIntegration_LimitPagination` | POST 50 events → GET /events?limit=10 returns last 10 |

### 5B. Security Integration (Attack Simulation)
| Test | Description |
|------|-------------|
| `TestSecurity_AuthRequired` | Set token, send requests without it → all endpoints except /health return 401 |
| `TestSecurity_AuthTokenInQuery` | Token in `?token=...` query param instead of header → 401 |
| `TestSecurity_PathTraversalViaURL` | GET `/hook/../../etc/passwd` → 404 (not path traversal) |
| `TestSecurity_PathTraversalEncoded` | GET `/hook/%2e%2e%2fetc%2fpasswd` → 404 |
| `TestSecurity_SQLInjectionPayload` | POST with `{"name": "'; DROP TABLE hooks; --"}` → 200 OK (stored as data, no SQL) |
| `TestSecurity_XSSPayload` | POST with `{"name": "<script>alert(1)</script>"}` → stored as-is, response has `nosniff` header |
| `TestSecurity_HugePayload` | POST 2 MiB body → server reads only 1 MiB, responds 200 or 400 (truncated JSON) |
| `TestSecurity_SlowLoris` | Open connection, send 1 byte per second → server times out after ReadHeaderTimeout (5s) |
| `TestSecurity_ManyConnections` | Open 100 concurrent POST requests → all succeed (no connection exhaustion with proper timeouts) |
| `TestSecurity_JSONBomb` | Send deeply nested JSON (depth=200) → rejected with 400 |
| `TestSecurity_NullByteInURL` | Request `/hook/Pre%00ToolUse` → 404 (null byte breaks routing) |
| `TestSecurity_HeaderInjection` | Send `Authorization: Bearer token\r\nX-Evil: injected` → no header injection |
| `TestSecurity_MethodOverrideHeader` | POST with `X-HTTP-Method-Override: DELETE` → still processes as POST (no method override) |
| `TestSecurity_ContentTypeManipulation` | POST with `Content-Type: text/xml` → still processes (handler ignores Content-Type, just parses JSON) |
| `TestSecurity_ResponseHeadersPresent` | All responses have `X-Content-Type-Options: nosniff` and `Cache-Control: no-store` |

---

## Phase 6: CI Workflow

**File:** `.github/workflows/test.yml`

```yaml
name: Test

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Run tests with race detector
        run: go test -race -count=1 -timeout 120s ./...
      - name: Run tests with coverage
        run: go test -coverprofile=coverage.out ./...
      - name: Print coverage summary
        run: go tool cover -func=coverage.out | tail -1
```

---

## Implementation Order (Most Value First)

| Priority | Phase | Estimated Tests | Rationale |
|----------|-------|-----------------|-----------|
| **P0** | Phase 1: Server unit tests | ~65 | Primary attack surface, most security-critical |
| **P0** | Phase 6: CI workflow | 1 file | Enables all future test enforcement |
| **P1** | Phase 5: Integration tests | ~20 | User-perspective validation, catches wiring bugs |
| **P2** | Phase 2: Monitor tests | ~35 | Ring buffer correctness + concurrency safety |
| **P2** | Phase 3: Hook client tests | ~40 | Input validation + security boundary |
| **P3** | Phase 4: Config edge cases | ~20 | Extends existing good coverage |

---

## Test Design Principles

1. **Table-driven tests** — All similar test cases use `[]struct{name, input, expected}` subtests with `t.Run()`
2. **Standard library only** — No testify/gomega; use `t.Errorf`/`t.Fatalf` per existing convention
3. **`httptest.NewServer`** for integration tests — real TCP connections, real middleware stack
4. **`httptest.NewRecorder`** for unit tests — fast, isolated, per-handler
5. **`t.Parallel()`** on all independent tests — faster CI, catches implicit shared state
6. **`-race` flag in CI** — catches data races in concurrent tests
7. **Helper functions** — `makeNestedJSON(depth)`, `makeEvent(hookType)`, `assertStatus(t, resp, code)` to reduce boilerplate
8. **No TUI testing** — TUI uses bubbletea which requires terminal; skip for now (low ROI)

---

## Attack Taxonomy Covered

| Attack Vector | Where Tested | Tests |
|--------------|-------------|-------|
| **Path Traversal** | server integration, isAlphaOnly | `../../etc/passwd`, URL-encoded variants |
| **JSON Bomb (DoS)** | server checkJSONDepth | depth 101, 200 |
| **Body Overflow (DoS)** | server HandleHook | 1 MiB, 2 MiB payloads |
| **Slowloris (DoS)** | integration | 1 byte/sec connection |
| **Connection Flood** | integration | 100 concurrent requests |
| **Auth Bypass** | AuthMiddleware | missing/wrong/malformed tokens |
| **SSRF** | discoverMonitorURL | `http://169.254.169.254/`, remote hosts |
| **Injection (SQL/XSS/Cmd)** | integration | Classic payloads stored but never executed |
| **Header Injection** | integration | CRLF in headers |
| **Timing Attack** | AuthMiddleware | constant-time comparison verification |
| **Race Condition** | monitor concurrent tests | add+read+close races |
| **Resource Exhaustion** | monitor ring buffer | 2000 events → bounded at 900 |
| **Unicode Abuse** | truncate, HandleHook | Multi-byte boundary, CJK, emoji |
| **Null Byte Injection** | isAlphaOnly, URL routing | `\x00` in hook type and URL |

---

## Expected Outcomes

- **Test count**: ~180 new tests across 5 files
- **Coverage target**: >80% for `server`, `monitor`, `config`; >60% for `hook-client` helpers
- **CI gate**: All PRs must pass `go test -race ./...`
- **Security confidence**: All OWASP Top 10 relevant vectors tested for this architecture
- **Concurrency safety**: Race detector validates all shared-state access patterns
