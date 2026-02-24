# Architecture — Claude Code Hooks Monitor

## Table of Contents

- [System Overview](#system-overview)
- [Project Structure](#project-structure)
- [Component Diagram](#component-diagram)
- [Data Flow](#data-flow)
- [Package Dependencies](#package-dependencies)
- [Hook Client (cmd/hook-client)](#hook-client-cmdhook-client)
- [Monitor Server (cmd/monitor)](#monitor-server-cmdmonitor)
- [TUI Subsystem (internal/tui)](#tui-subsystem-internaltui)
- [Config & Toggle System](#config--toggle-system)
- [Concurrency Model](#concurrency-model)
- [Platform Abstraction (internal/platform)](#platform-abstraction-internalplatform)
- [HTTP API](#http-api)
- [Slash Command (/monitor-hooks)](#slash-command-monitor-hooks)
- [Security Notes](#security-notes)
- [Environment Variables](#environment-variables)
- [Extension Points](#extension-points)
- [Performance](#performance)

---

## System Overview

```
 ┌──────────────────────────────────────────────────────────────────────┐
 │                        Claude Code CLI                               │
 │                                                                      │
 │  User prompt → Tools → Hooks fire at each lifecycle point            │
 │  Each hook executes the configured command with JSON on stdin         │
 └──────────────┬───────────────────────────────────────────────────────┘
                │ stdin: JSON payload
                │ (hook_event_name, session_id, tool_name, etc.)
                ▼
 ┌──────────────────────────────────────────────────────────────────────┐
 │              hook-client  (Go binary, cmd/hook-client)               │
 │                                                                      │
 │  1. Read JSON from stdin (bounded: 1 MiB max)                       │
 │  2. Parse hook_event_name from payload                               │
 │  3. Check hook_monitor.conf — is this hook enabled?                  │
 │     • disabled → exit 0 immediately (no network call)                │
 │     • enabled  → continue                                            │
 │  4. Add _monitor metadata (timestamp, project_dir, is_remote)        │
 │  5. Discover monitor URL (env var → port file → default :8080)       │
 │  6. HTTP POST to /hook/{hookType}                                    │
 │  7. Exit 0 (never blocks Claude Code)                                │
 └──────────────┬───────────────────────────────────────────────────────┘
                │ HTTP POST /hook/{hookType}
                │ (JSON body, optional Bearer token)
                ▼
 ┌──────────────────────────────────────────────────────────────────────┐
 │            Go Monitor Server  (cmd/monitor)                          │
 │                                                                      │
 │  ┌─────────────────┐    ┌──────────────────┐    ┌────────────────┐  │
 │  │  HTTP Handlers   │───▶│   HookMonitor    │───▶│  Console Log   │  │
 │  │  (server pkg)    │    │   (monitor pkg)  │    │  (colorized)   │  │
 │  └─────────────────┘    │                  │    └────────────────┘  │
 │                          │  Ring buffer     │              OR        │
 │                          │  (1000 events)   │    ┌────────────────┐  │
 │                          │  Stats counters  │───▶│  Bubble Tea    │  │
 │                          │  RWMutex         │    │  TUI (--ui)    │  │
 │                          └──────────────────┘    └────────────────┘  │
 └──────────────────────────────────────────────────────────────────────┘
```

---

## Project Structure

```
claude-hooks-monitor/
├── cmd/
│   ├── monitor/                     # Main server binary
│   │   └── main.go                  # Entrypoint: flags, HTTP setup, mode dispatch
│   └── hook-client/                 # Hook client binary
│       └── main.go                  # Stdin reader, config check, HTTP forwarder
├── internal/
│   ├── config/                      # Shared config package
│   │   └── config.go                # AllHookTypes, INI read/write, AtomicWriteFile
│   ├── hookevt/                     # Shared types
│   │   └── hookevt.go               # HookEvent struct (used by all packages)
│   ├── monitor/                     # Core event processing
│   │   └── monitor.go               # HookMonitor: ring buffer, stats, TUI channel
│   ├── server/                      # HTTP layer
│   │   └── server.go                # Handlers, middleware (auth, security headers)
│   ├── platform/                    # OS-specific code
│   │   ├── lock.go                  # ShowRunningInstance (shared diagnostics)
│   │   ├── lock_unix.go             # flock-based locking (Linux/macOS)
│   │   ├── lock_windows.go          # LockFileEx-based locking (Windows)
│   │   ├── signals_unix.go          # SIGINT + SIGTERM
│   │   └── signals_windows.go       # SIGINT only
│   └── tui/                         # Interactive tree UI
│       ├── model.go                 # Bubble Tea model, key handling, viewport
│       ├── tree.go                  # Tree data structures (Session/Request/EventNode)
│       ├── processor.go             # Event → tree builder with Pre/Post pairing
│       ├── detail.go                # Detail pane renderer
│       └── styles.go                # Lipgloss styles, row rendering
├── hooks/
│   ├── hook-client[.exe]            # Compiled client binary (gitignored)
│   ├── hook_monitor.conf            # Toggle: enable/disable individual hooks
│   ├── .monitor-port                # Runtime: port file (gitignored)
│   └── .monitor-lock                # Runtime: lock file (gitignored)
├── .claude/
│   ├── commands/
│   │   └── monitor-hooks.md         # /monitor-hooks slash command
│   └── settings.json                # Hook config + Claude Code permissions
├── go.mod / go.sum
├── Makefile
└── [docs: README, INSTALLME, EXAMPLES, ARCHITECTURE]
```

---

## Component Diagram

```
 ┌─────────────────────────────────────────────────────────────────┐
 │                        cmd/monitor/main.go                      │
 │                                                                 │
 │  Wires together all internal packages:                          │
 │  • Creates HookMonitor (with optional TUI event channel)        │
 │  • Registers HTTP handlers on dedicated ServeMux                │
 │  • Wraps with SecurityHeaders + optional AuthMiddleware         │
 │  • Acquires single-instance lock via platform.AcquireLock       │
 │  • Listens on configured port (fallback to OS-assigned)         │
 │  • Writes port file for hook-client discovery                   │
 │  • Dispatches to Console mode or TUI mode                       │
 └──────┬──────────┬──────────┬──────────┬─────────────────────────┘
        │          │          │          │
        ▼          ▼          ▼          ▼
 ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
 │ internal/ │ │ internal/ │ │ internal/ │ │ internal/ │
 │ monitor   │ │ server   │ │ platform │ │ tui      │
 │           │ │          │ │          │ │          │
 │ HookMon-  │ │ Handle-  │ │ Acquire- │ │ Run()    │
 │ itor      │ │ Hook()   │ │ Lock()   │ │ Model    │
 │ AddEvent  │ │ Handle-  │ │ Shutdown │ │ Event-   │
 │ GetStats  │ │ Stats()  │ │ Signals  │ │ Processor│
 │ GetEvents │ │ Handle-  │ │ Show-    │ │ FlatRow  │
 │ MaxEvents │ │ Events() │ │ Running  │ │ Detail   │
 │ MaxBody-  │ │ Handle-  │ │ Instance │ │ Styles   │
 │ Len       │ │ Health() │ │          │ │          │
 │           │ │ Security │ │          │ │          │
 │           │ │ Headers  │ │          │ │          │
 │           │ │ Auth-    │ │          │ │          │
 │           │ │ Middle-  │ │          │ │          │
 │           │ │ ware     │ │          │ │          │
 └──────────┘ └──────────┘ └──────────┘ └──────────┘
        ▲          ▲                          ▲
        │          │                          │
        └──────────┴──────────────────────────┘
                          │
                   ┌──────────┐
                   │ internal/ │
                   │ hookevt  │
                   │          │
                   │ HookEvent│
                   │ struct   │
                   └──────────┘
```

---

## Data Flow

### Sequence: Hook Event Lifecycle

```
 Claude Code          hook-client         Config File       Monitor Server       HookMonitor
     │                    │                    │                  │                   │
     │  stdin (JSON)      │                    │                  │                   │
     │───────────────────▶│                    │                  │                   │
     │                    │                    │                  │                   │
     │                    │  read hook_monitor │                  │                   │
     │                    │  .conf             │                  │                   │
     │                    │───────────────────▶│                  │                   │
     │                    │                    │                  │                   │
     │                    │  enabled? yes/no   │                  │                   │
     │                    │◀───────────────────│                  │                   │
     │                    │                    │                  │                   │
     │                    │─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─(if disabled: exit 0)               │
     │                    │                    │                  │                   │
     │                    │  POST /hook/{type} │                  │                   │
     │                    │──────────────────────────────────────▶│                   │
     │                    │                    │                  │                   │
     │                    │                    │                  │  AddEvent(event)  │
     │                    │                    │                  │──────────────────▶│
     │                    │                    │                  │                   │
     │                    │                    │                  │                   │──┐
     │                    │                    │                  │                   │  │ Ring buffer
     │                    │                    │                  │                   │  │ append +
     │                    │                    │                  │                   │  │ stats++
     │                    │                    │                  │                   │◀─┘
     │                    │                    │                  │                   │
     │                    │                    │                  │                   │──┐ if eventCh:
     │                    │                    │                  │                   │  │ send to TUI
     │                    │                    │                  │                   │◀─┘ else: log
     │                    │                    │                  │                   │
     │                    │  200 OK            │                  │                   │
     │                    │◀──────────────────────────────────────│                   │
     │                    │                    │                  │                   │
     │  exit 0            │                    │                  │                   │
     │◀───────────────────│                    │                  │                   │
     │                    │                    │                  │                   │
```

### Sequence: PreToolUse → PostToolUse Pairing (TUI)

```
 Claude Code          hook-client       Monitor Server      EventProcessor        TUI Model
     │                    │                  │                   │                    │
     │  PreToolUse        │                  │                   │                    │
     │  (Bash: echo hi)   │                  │                   │                    │
     │───────────────────▶│  POST            │                   │                    │
     │                    │─────────────────▶│  AddEvent         │                    │
     │                    │                  │──────────────────▶│                    │
     │                    │                  │                   │                    │
     │                    │                  │                   │  Process(event)    │
     │                    │                  │   eventCh ────────│───────────────────▶│
     │                    │                  │                   │  pendingPre[Bash]  │
     │                    │                  │                   │  = [this node]     │
     │                    │                  │                   │                    │
     │  (executes Bash)   │                  │                   │                    │
     │                    │                  │                   │                    │
     │  PostToolUse       │                  │                   │                    │
     │  (Bash: ok)        │                  │                   │                    │
     │───────────────────▶│  POST            │                   │                    │
     │                    │─────────────────▶│  AddEvent         │                    │
     │                    │                  │──────────────────▶│                    │
     │                    │                  │                   │                    │
     │                    │                  │                   │  Process(event)    │
     │                    │                  │   eventCh ────────│───────────────────▶│
     │                    │                  │                   │  FIFO match:       │
     │                    │                  │                   │  pre.PostPair =    │
     │                    │                  │                   │    this Post node  │
     │                    │                  │                   │                    │
     │                    │                  │                   │                    │
     │                    │                  │                   │         Tree View: │
     │                    │                  │                   │         ▼ Session  │
     │                    │                  │                   │           ▼ Prompt │
     │                    │                  │                   │             ▶ Bash │
     │                    │                  │                   │           (expand) │
     │                    │                  │                   │             ▼ Bash │
     │                    │                  │                   │               Post │
```

---

## Package Dependencies

```
                    ┌──────────────────────┐
                    │   cmd/monitor/main   │
                    │      (package main)  │
                    └──┬───┬───┬───┬───────┘
                       │   │   │   │
          ┌────────────┘   │   │   └────────────┐
          ▼                ▼   ▼                 ▼
  ┌──────────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐
  │   internal/   │  │ internal/ │  │ internal/ │  │  internal/   │
  │   monitor     │  │ server   │  │ platform │  │  tui         │
  └──────┬───────┘  └────┬─────┘  └──────────┘  └──────┬──────┘
         │               │                              │
         │               │                              │
         ▼               ▼                              ▼
  ┌────────────────────────────────────────────────────────────┐
  │                    internal/hookevt                         │
  │                    (HookEvent struct)                       │
  └────────────────────────────────────────────────────────────┘

  ┌──────────────────────┐
  ┌──────────────────────┐
  │  cmd/hook-client/main │  (imports internal/config for
  │     (package main)    │   AllHookTypes + AtomicWriteFile)
  └──────────────────────┘
```

**Key design constraint:** Go's `internal/` directory provides compiler-enforced
encapsulation. No external module can import any `internal/` package. The two
binaries (`cmd/monitor` and `cmd/hook-client`) share the `internal/config` package
for hook type definitions and atomic file writes. The `hook-client` also provides
an `install-hooks` subcommand that registers all hooks in `~/.claude/settings.json`
— eliminating the previous Python dependency for installation.

---

## Hook Client (cmd/hook-client)

### Subcommand Dispatch

```
                    ┌─────────┐
                    │  START  │
                    └────┬────┘
                         │
                         ▼
               ┌───────────────────┐
               │ os.Args[1] ==     │       ┌─────────────────────┐
               │ "install-hooks"?  │──yes─▶│ runInstallHooks()   │
               └────────┬──────────┘       │ Read/write          │
                        │ no               │ ~/.claude/settings  │
                        ▼                  │ .json via           │
                (normal hook flow)         │ config.AtomicWrite  │
                                           └─────────────────────┘
```

### State Machine: Request Processing

```
                    ┌─────────┐
                    │  START  │
                    └────┬────┘
                         │
                         ▼
                ┌──────────────────┐
                │  Read stdin JSON  │
                │  (max 1 MiB)     │
                └────────┬─────────┘
                         │
                         ▼
                ┌──────────────────┐
                │ Parse JSON       │──── invalid ───▶ wrap as raw_input
                │ extract hookType │                  with error field
                └────────┬─────────┘
                         │
                         ▼
               ┌───────────────────┐       ┌────────┐
               │ isHookEnabled()   │──no──▶│ exit 0 │
               │ read conf file    │       └────────┘
               └────────┬──────────┘
                        │ yes
                        ▼
               ┌───────────────────┐
               │ Add _monitor      │
               │ metadata          │
               └────────┬──────────┘
                        │
                        ▼
               ┌───────────────────┐
               │ Discover URL      │
               │ env → port file   │
               │ → default :8080   │
               └────────┬──────────┘
                        │
                        ▼
               ┌───────────────────┐       ┌────────┐
               │ POST /hook/{type} │──err─▶│ exit 0 │
               │ with timeout      │       └────────┘
               └────────┬──────────┘
                        │ ok
                        ▼
                   ┌────────┐
                   │ exit 0 │
                   └────────┘
```

**Safety invariant:** Every code path exits 0. The client must never block
Claude Code — connection errors, timeouts, and malformed input are all
silently swallowed.

### URL Discovery Priority

```
  1. HOOK_MONITOR_URL env var
     │
     ├─ valid http + loopback host? ─────▶ use it
     └─ invalid or non-loopback? ────────▶ skip (exit 0)

  2. .monitor-port file (XDG dir → binary-relative dir)
     │
     ├─ exists + valid port number? ─────▶ http://localhost:{port}
     └─ missing or invalid? ─────────────▶ fall through

  3. No URL found → skip (exit 0, never block Claude)
```

---

## Monitor Server (cmd/monitor)

### Startup Sequence

```
  ┌────────────┐
  │ Parse flags │  --ui flag selects TUI vs console mode
  └─────┬──────┘
        │
        ▼
  ┌───────────────────────┐
  │ Resolve port/lock     │  PORT_FILE env or XDG dir (~/.config/claude-hooks-monitor/)
  │ + config file paths   │  Config via HOOK_MONITOR_CONFIG env → XDG → fallback
  └─────┬─────────────────┘
        │
        ▼
  ┌───────────────────────┐       ┌───────────────────────────┐
  │ platform.AcquireLock  │──fail▶│ ShowRunningInstance       │
  │ (flock / LockFileEx)  │       │ (shows PID, URL, stats)  │
  └─────┬─────────────────┘       │ then exit 1              │
        │ ok                      └───────────────────────────┘
        ▼
  ┌───────────────────────┐
  │ Remove stale port file │  Lock proves we're sole instance
  └─────┬─────────────────┘
        │
        ▼
  ┌───────────────────────┐
  │ Create HookMonitor    │  If --ui: with eventCh (buffered 256)
  │ Register HTTP handlers │  15 hook endpoints + /stats + /events + /health
  │ Apply middleware       │  SecurityHeaders + optional AuthMiddleware
  └─────┬─────────────────┘
        │
        ▼
  ┌───────────────────────┐       ┌───────────────────────────┐
  │ net.Listen :PORT      │──fail▶│ Retry on :0 (OS-assigned) │
  └─────┬─────────────────┘       └─────┬─────────────────────┘
        │                               │
        ▼◀──────────────────────────────┘
  ┌───────────────────────┐
  │ Write port file       │  For hook-client discovery
  │ Register signal       │  platform.ShutdownSignals (SIGINT, SIGTERM)
  │ handler               │
  └─────┬─────────────────┘
        │
        ├───────────────────────────────┐
        │ --ui mode                     │ console mode
        ▼                               ▼
  ┌───────────────┐             ┌───────────────────┐
  │ go srv.Serve  │             │ printBanner()     │
  │ tui.Run(ctx)  │             │ srv.Serve(ln)     │
  │ (blocks)      │             │ (blocks)          │
  └───────┬───────┘             └────────┬──────────┘
          │                              │
          ▼                              ▼
  ┌─────────────────────────────────────────────┐
  │ Deferred cleanup:                            │
  │   cancel() → server.Shutdown → rm port file  │
  │   → lockFd.Close → rm lock file              │
  └─────────────────────────────────────────────┘
```

---

## TUI Subsystem (internal/tui)

### Class Diagram: Tree Data Model

```
  ┌──────────────────────────────┐
  │        EventProcessor        │
  ├──────────────────────────────┤
  │ - sessions    []*Session     │
  │ - sessionMap  map → *Session │
  │ - currentSession *Session    │
  │ - currentRequest *UserRequest│
  │ - pendingPre  map → []*Event │
  ├──────────────────────────────┤
  │ + Process(HookEvent)         │
  │   []*Session                 │
  └──────────────┬───────────────┘
                 │ produces
                 ▼
  ┌──────────────────────────────┐
  │          Session             │
  ├──────────────────────────────┤        ┌──────────────────────────────┐
  │ - ID        string           │        │       UserRequest            │
  │ - StartTime time.Time        │        ├──────────────────────────────┤
  │ - Requests  []*UserRequest ──│───────▶│ - Prompt    string           │
  │ - Expanded  bool             │   1:N  │ - Timestamp time.Time        │
  └──────────────────────────────┘        │ - Events    []*EventNode  ──│──┐
                                          │ - Expanded  bool             │  │
                                          └──────────────────────────────┘  │
                                                                            │ 1:N
                                          ┌─────────────────────────────────┘
                                          ▼
                                  ┌──────────────────────────────┐
                                  │        EventNode             │
                                  ├──────────────────────────────┤
                                  │ - HookType  string           │
                                  │ - Timestamp time.Time        │
                                  │ - ToolName  string           │
                                  │ - Summary   string           │
                                  │ - Data      map[string]any   │
                                  │ - PostPair  *EventNode ──────│──┐  0:1
                                  │ - Expanded  bool             │  │  (Pre→Post link)
                                  └──────────────────────────────┘  │
                                          ▲                         │
                                          └─────────────────────────┘
```

### Tree Rendering Pipeline

```
  HookEvent stream
       │
       ▼
  EventProcessor.Process()
       │ groups by session_id, pairs Pre/Post by tool_name (FIFO)
       ▼
  []*Session  (tree structure)
       │
       ▼
  FlattenTree()
       │ walks expanded nodes depth-first
       ▼
  []FlatRow  (flat list for viewport)
       │
       ▼
  renderRow()  per visible row
       │ indent by depth, expand/collapse icon, hook-type color
       ▼
  Bubble Tea View()
       │ header + scrolled rows + optional detail pane + footer
       ▼
  Terminal output (alt-screen)
```

### TUI Layout

```
  ┌────────────────────────────────────────────────────────────────┐
  │  Claude Hooks Monitor  │  Port 8080  │  Events: 42            │  ← header
  ├────────────────────────────────────────────────────────────────┤
  │  ▼ 14:32:05 Session [abc123def456]                            │  depth 0
  │    ▼ 14:32:06 What files handle routing?                      │  depth 1
  │      ▶ 14:32:07 Glob: **/*.ts                                 │  depth 2
  │      ▼ 14:32:08 Read: src/router.ts                           │  depth 2
  │        14:32:08 Read completed                                 │  depth 3
  │      ▶ 14:32:09 Bash: grep -r "route"                         │  depth 2
  │    ▼ 14:32:15 Add authentication middleware                    │  depth 1
  │      ▶ 14:32:16 Write: src/middleware/auth.ts                  │  depth 2
  ├──────────────────────────────────────────────────────────(─────┤  ← divider
  │  PreToolUse · Read                                             │  ← detail pane
  │  ───────────────────────────────────                           │
  │  Time:        14:32:08.123                                     │
  │  ── Input ──────────────────────────                           │
  │  File:        src/router.ts                                    │
  │  ── Result (PostToolUse) ───────────                           │
  │  Status:      Read completed                                   │
  ├────────────────────────────────────────────────────────────────┤
  │  esc/i: close  j/k: scroll detail                              │  ← footer
  └────────────────────────────────────────────────────────────────┘
```

### Key Bindings State Machine

```
  ┌──────────────────┐                    ┌───────────────────┐
  │   TREE MODE      │                    │   DETAIL MODE     │
  │                  │        i           │                   │
  │  j/k: navigate   │───────────────────▶│  j/k: scroll pane │
  │  h/l: collapse/  │                    │  esc/i: close     │
  │       expand     │◀───────────────────│                   │
  │  space: toggle   │     esc / i        │                   │
  │  g/G: top/bottom │                    │                   │
  │  q: quit         │                    │  q: quit          │
  └──────────────────┘                    └───────────────────┘
```

---

## Config & Toggle System

### Config File Format (hooks/hook_monitor.conf)

```ini
[hooks]
# Toggle individual hooks on/off.
# Set to "yes" to monitor, "no" to skip.
# Missing entries default to "yes" (fail-open).
# Changes take effect immediately — no restart needed.

SessionStart = yes
PreToolUse = no
PostToolUse = no
...
```

### Config Read Paths

Two independent readers parse the same INI file:

```
  hook-client (Go)                    /monitor-hooks (Bash)
  ──────────────────                  ──────────────────────
  isHookEnabled()                     parse_hooks_section()
  │                                   │
  │  os.ReadFile (single-syscall)     │  while IFS= read -r
  │  line-by-line                     │  line-by-line
  │  [hooks] section detection        │  [hooks] section detection
  │  key = value parsing              │  key = value parsing
  │  case-insensitive key match       │  case-insensitive key match
  │  inline comment stripping         │  inline comment stripping
  │  last-wins for duplicates         │  PascalCase key normalization
  │  fail-open: missing → enabled     │  stores in associative array
  │                                   │  (declare -A HOOK_CFG)
  └──▶ bool                           └──▶ HOOK_CFG[key]=val
```

### Slash Command State Machine (/monitor-hooks)

```
  /monitor-hooks {args}
        │
        ▼
  ┌─────────────────┐
  │ Parse ARGUMENTS │  read -r SUBCMD REST _
  └────────┬────────┘
           │
     ┌─────┴──────────────────────────────────────┐
     │         │          │         │        │     │
     ▼         ▼          ▼         ▼        ▼     ▼
  activate  deactivate  status  show-all  help   *other*
     │         │          │         │        │     │
     ▼         ▼          ▼         ▼        ▼     ▼
  set_all   set_all    parse      parse   print  resolve_
  _hooks    _hooks     config     config  help   hook_name
  ("yes")   ("no")     show       audit   text      │
     │         │       ON/OFF     +extra        ┌───┴───┐
     ▼         ▼                  entries       │       │
  show_     show_                            "on"    "off"
  status    status                              │       │
                                                ▼       ▼
                                            set_hook  set_hook
                                            ("yes")   ("no")
```

---

## Concurrency Model

```
  Main goroutine                    Signal goroutine           HTTP goroutines
  ──────────────                    ────────────────           ────────────────

  ┌─────────────┐                   ┌──────────────┐
  │ Setup +     │                   │ signal.Notify│
  │ Serve/TUI   │                   │ (SIGINT,     │
  └──────┬──────┘                   │  SIGTERM)    │
         │                          └──────┬───────┘
         │                                 │             ┌────────────────┐
         │                                 │             │ HandleHook()   │
         │  ctx.Cancel ◀───────────────────┤             │                │
         │                                 │             │  mu.Lock()     │
         │  srv.Shutdown ◀─────────────────┤             │  append event  │
         │                                 │             │  stats++       │
         │                                 │             │  eventCh <-    │
         ▼                                 │             │  mu.Unlock()   │
  ┌─────────────┐                          │             │                │
  │ Deferred    │                          │             │  (concurrent   │
  │ cleanup     │                          │             │   with other   │
  │             │                          │             │   HTTP reqs)   │
  └─────────────┘                          │             └────────────────┘
                                           │
                                           │             ┌────────────────┐
                                           │             │ HandleStats()  │
                                           │             │ HandleEvents() │
                                           │             │                │
                                           │             │  mu.RLock()    │
                                           │             │  copy data     │
                                           │             │  mu.RUnlock()  │
                                           │             └────────────────┘

  Thread safety: sync.RWMutex protects events[] and stats map.
  Writes (AddEvent) use Lock(); reads (GetStats, GetEvents) use RLock().
  Multiple concurrent readers allowed; writers are exclusive.
```

### TUI Event Channel

```
  HTTP goroutine          HookMonitor              TUI goroutine
  ─────────────           ───────────              ─────────────
       │                       │                        │
       │  AddEvent(ev)         │                        │
       │──────────────────────▶│                        │
       │                       │                        │
       │                       │  mu.Lock()             │
       │                       │  append + stats        │
       │                       │                        │
       │                       │  eventCh <- ev         │
       │                       │  (non-blocking)        │
       │                       │   │                    │
       │                       │   ├─ success ──────────│──▶ waitForEvent()
       │                       │   │                    │    returns EventMsg
       │                       │   └─ full: drop,       │
       │                       │     Dropped.Add(1)     │
       │                       │                        │
       │                       │  mu.Unlock()           │
       │                       │                        │

  Channel buffer: 256. If TUI can't keep up, events are dropped
  (counted in atomic Dropped counter, shown in TUI header).
```

---

## Platform Abstraction (internal/platform)

```
  ┌──────────────────────────────────────────────────────────────┐
  │                     internal/platform                        │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  lock.go (all platforms)                                     │
  │  ├── ShowRunningInstance(lockPath, portPath)                  │
  │  │   Reads PID + port, fetches /stats from running instance  │
  │                                                              │
  │  lock_unix.go       //go:build !windows                      │
  │  ├── AcquireLock(lockPath, portPath) *os.File                │
  │  │   Uses syscall.Flock (LOCK_EX | LOCK_NB)                 │
  │  │   Non-blocking: fails immediately if lock held            │
  │  │   Writes PID to lock file for diagnostics                 │
  │                                                              │
  │  lock_windows.go    //go:build windows                       │
  │  ├── AcquireLock(lockPath, portPath) *os.File                │
  │  │   Uses windows.LockFileEx (LOCKFILE_EXCLUSIVE_LOCK |      │
  │  │                             LOCKFILE_FAIL_IMMEDIATELY)    │
  │                                                              │
  │  signals_unix.go    //go:build !windows                      │
  │  ├── ShutdownSignals = []os.Signal{SIGINT, SIGTERM}          │
  │                                                              │
  │  signals_windows.go //go:build windows                       │
  │  ├── ShutdownSignals = []os.Signal{os.Interrupt}             │
  │                                                              │
  └──────────────────────────────────────────────────────────────┘

  Build tags ensure only the correct platform file compiles.
  No runtime if/else branching for platform differences.
```

---

## HTTP API

### Endpoint Map

```
  ┌──────────────────────────┬────────┬─────────────────────────────────┐
  │ Endpoint                 │ Method │ Description                     │
  ├──────────────────────────┼────────┼─────────────────────────────────┤
  │ /hook/{HookType}         │ POST   │ Receive hook event (15 types)   │
  │ /stats                   │ GET    │ Aggregate hook counts + total   │
  │ /events                  │ GET    │ Last N events (?limit=100)      │
  │ /health                  │ GET    │ Liveness check (exempt from     │
  │                          │        │ auth for monitoring probes)     │
  └──────────────────────────┴────────┴─────────────────────────────────┘
```

### Middleware Chain

```
  Incoming request
       │
       ▼
  SecurityHeaders          X-Content-Type-Options: nosniff
       │                   Cache-Control: no-store
       ▼
  AuthMiddleware           If HOOK_MONITOR_TOKEN set:
       │                     /health → pass through
       │                     others  → require "Bearer {token}"
       ▼
  ServeMux routing
       │
       ├─ /hook/{type} ──▶ HandleHook()   ──▶ AddEvent()
       ├─ /stats ────────▶ HandleStats()  ──▶ GetStats()
       ├─ /events ───────▶ HandleEvents() ──▶ GetEvents()
       └─ /health ───────▶ HandleHealth()
```

---

## Security Notes

- **Localhost only:** Server binds to `127.0.0.1`. Not accessible from network.
- **No data persistence:** Events are in-memory only. Lost on restart.
- **Optional auth:** Set `HOOK_MONITOR_TOKEN` for Bearer token authentication.
  `/health` is exempt for monitoring probes.
- **Input bounds:** Stdin capped at 1 MiB (hook-client), request body capped at
  1 MiB (server). Prevents memory exhaustion from malformed payloads.
- **URL validation:** hook-client refuses non-loopback `HOOK_MONITOR_URL` targets
  and URL-escapes hook types to prevent path traversal.
- **Port file path validation:** Server rejects absolute paths and `..` in `PORT_FILE`.
- **Stdin is sensitive:** Tool inputs may include file contents, commands, secrets.
  The monitor logs everything — do not run in shared/public environments.

---

## Environment Variables

| Variable | Default | Used By | Purpose |
|----------|---------|---------|---------|
| `PORT` | 8080 | Go server | Listen port |
| `PORT_FILE` | (XDG dir)/.monitor-port | Go server | Port file path override |
| `HOOK_MONITOR_URL` | (auto-discover) | hook-client | Override monitor URL |
| `HOOK_MONITOR_CONFIG` | (auto-discover) | Both | Override config file path |
| `HOOK_MONITOR_TOKEN` | (none) | Both | Bearer token for auth |
| `HOOK_TIMEOUT` | 2 | hook-client | HTTP timeout (seconds, max 10) |
| `CLAUDE_PROJECT_DIR` | (set by Claude) | hook-client, slash cmd | Project root path |
| `CLAUDE_PLUGIN_ROOT` | (set by Claude) | hook-client | Plugin context |
| `CLAUDE_CODE_REMOTE` | (set by Claude) | hook-client | Remote session flag |
| `XDG_CONFIG_HOME` | ~/.config | Both | Base for config directory |

---

## Extension Points

| What | How |
|------|-----|
| Add persistence | Store `HookEvent` to SQLite in `AddEvent()` |
| Add web dashboard | Serve HTML from Go, add WebSocket for real-time updates |
| Filter events | Add query params to `/events` (e.g., `?type=PreToolUse`) |
| Export data | Add `/export` endpoint returning CSV or JSONL |
| Multi-project | Add project ID to `HookEvent`, partition storage |
| Custom handlers | Add new endpoints in `internal/server` |

---

## Performance

**hook-client overhead:** ~5-10ms per invocation (compiled Go binary, single
HTTP POST). Non-blocking — Claude Code doesn't wait for it.

**Go server throughput:** Handles 1000+ req/sec easily. The bottleneck is
console I/O (printing), not computation.

**Memory:** Ring buffer capped at 1000 events. Each event is ~1-5 KB depending
on payload. Max memory: ~5 MB for events.

**TUI channel:** Buffered at 256 events. If the TUI can't keep up, events are
dropped (counted, not lost from the ring buffer — only from the TUI display).
