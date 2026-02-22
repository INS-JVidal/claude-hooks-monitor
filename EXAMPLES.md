# Examples — Claude Code Hooks Monitor

## Server Startup

```
╔══════════════════════════════════════════════════════════════╗
║           Claude Code Hooks Monitor v0.4.6                    ║
╚══════════════════════════════════════════════════════════════╝

  Registered 15 hook endpoints
  Endpoints: /stats  /events  /health
  Listening on http://localhost:8080

  Waiting for hook events...
```

## Hook Event Examples

Each hook type is displayed with a unique color and separator:

### SessionStart (Green)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ SessionStart
  🕐 14:23:01.000
  {
    "session_id": "abc123-def456",
    "transcript_path": "/home/user/.claude/projects/.../transcript.jsonl",
    "cwd": "/home/user/my-project",
    "permission_mode": "default",
    "hook_event_name": "SessionStart"
  }
════════════════════════════════════════════════════════════════════════════════
```

### PreToolUse (Yellow)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ PreToolUse
  🕐 14:23:05.123
  {
    "session_id": "abc123-def456",
    "hook_event_name": "PreToolUse",
    "tool_name": "Write",
    "tool_input": {
      "file_path": "fibonacci.py",
      "content": "def fib(n):\n    if n <= 1:\n        return n\n    return fib(n-1) + fib(n-2)"
    },
    "_monitor": {
      "timestamp": "2026-02-20T14:23:05.123456+00:00",
      "project_dir": "/home/user/my-project",
      "plugin_root": "",
      "is_remote": false
    }
  }
════════════════════════════════════════════════════════════════════════════════
```

### PostToolUse (Cyan)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ PostToolUse
  🕐 14:23:05.456
  {
    "session_id": "abc123-def456",
    "hook_event_name": "PostToolUse",
    "tool_name": "Write",
    "tool_input": { "file_path": "fibonacci.py" },
    "tool_response": { "success": true, "bytes_written": 128 }
  }
════════════════════════════════════════════════════════════════════════════════
```

### PostToolUseFailure (Bright Red)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ PostToolUseFailure
  🕐 14:23:06.789
  {
    "hook_event_name": "PostToolUseFailure",
    "tool_name": "Bash",
    "tool_input": { "command": "python nonexistent.py" },
    "tool_response": {
      "exit_code": 1,
      "stdout": "",
      "stderr": "python: can't open file 'nonexistent.py': No such file or directory"
    }
  }
════════════════════════════════════════════════════════════════════════════════
```

### UserPromptSubmit (Magenta)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ UserPromptSubmit
  🕐 14:23:03.000
  {
    "hook_event_name": "UserPromptSubmit",
    "prompt": "Create a Python script that calculates fibonacci numbers"
  }
════════════════════════════════════════════════════════════════════════════════
```

### Stop (Red)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ Stop
  🕐 14:23:10.000
  {
    "hook_event_name": "Stop",
    "stop_hook_active": true,
    "last_assistant_message": "I've completed the fibonacci implementation."
  }
════════════════════════════════════════════════════════════════════════════════
```

### SessionEnd (Red, Bold)

```
════════════════════════════════════════════════════════════════════════════════
  ⚡ SessionEnd
  🕐 14:25:06.000
  {
    "hook_event_name": "SessionEnd",
    "session_id": "abc123-def456",
    "reason": "user_exit"
  }
════════════════════════════════════════════════════════════════════════════════
```

## API Response Examples

### GET /health

```json
{
  "status": "healthy",
  "time": "2026-02-20T14:23:45+00:00"
}
```

### GET /stats

```json
{
  "stats": {
    "SessionStart": 1,
    "UserPromptSubmit": 1,
    "PreToolUse": 3,
    "PostToolUse": 3,
    "PostToolUseFailure": 1,
    "Notification": 1,
    "PermissionRequest": 1,
    "Stop": 1,
    "SubagentStart": 1,
    "SubagentStop": 1,
    "TeammateIdle": 1,
    "TaskCompleted": 1,
    "ConfigChange": 1,
    "PreCompact": 1,
    "SessionEnd": 1
  },
  "total_hooks": 19
}
```

### GET /events?limit=2

```json
{
  "events": [
    {
      "hook_type": "Stop",
      "timestamp": "2026-02-20T14:23:10.000Z",
      "data": {
        "hook_event_name": "Stop",
        "stop_hook_active": true,
        "last_assistant_message": "Done."
      }
    },
    {
      "hook_type": "SessionEnd",
      "timestamp": "2026-02-20T14:25:06.000Z",
      "data": {
        "hook_event_name": "SessionEnd",
        "reason": "user_exit"
      }
    }
  ],
  "count": 2
}
```

## Color Legend

| Hook Type | Color | Meaning |
|-----------|-------|---------|
| SessionStart | Green, Bold | Session lifecycle start |
| SessionEnd | Red, Bold | Session lifecycle end |
| UserPromptSubmit | Magenta, Bold | User interaction |
| PreToolUse | Yellow, Bold | Tool about to execute |
| PostToolUse | Cyan, Bold | Tool completed successfully |
| PostToolUseFailure | Bright Red, Bold | Tool execution failed |
| Notification | Blue, Bold | System notification |
| PermissionRequest | White, Bold | Permission needed |
| Stop | Red | Claude stopping |
| SubagentStart | Bright Cyan | Subagent launched |
| SubagentStop | Bright Cyan, Bold | Subagent completed |
| TeammateIdle | Bright Blue | Teammate idle |
| TaskCompleted | Bright Green | Task completed |
| ConfigChange | Bright Yellow | Config changed |
| PreCompact | Bright Magenta | Context compaction |

## Full Session Example

A typical session flow when asking Claude to write a file:

```
1. SessionStart     → session begins
2. UserPromptSubmit → "Create fibonacci.py"
3. PreToolUse       → Write tool about to create fibonacci.py
4. PostToolUse      → Write succeeded (128 bytes)
5. PreToolUse       → Bash tool about to run "python fibonacci.py"
6. PostToolUse      → Bash succeeded (exit 0)
7. Stop             → Claude done responding
8. SessionEnd       → session terminates
```

In the monitor, each event appears as a colorized block with full JSON data, making it easy to trace the complete execution flow.

## Slash Command Examples

### /monitor-hooks status

```
Monitor: STOPPED

Hook Configuration (/home/user/claude-hooks-monitor/hooks/hook_monitor.conf):
  SessionStart           OFF
  UserPromptSubmit       OFF
  PreToolUse             OFF
  PostToolUse            OFF
  PostToolUseFailure     OFF
  PermissionRequest      OFF
  Notification           OFF
  SubagentStart          OFF
  SubagentStop           OFF
  Stop                   OFF
  TeammateIdle           OFF
  TaskCompleted          OFF
  ConfigChange           OFF
  PreCompact             OFF
  SessionEnd             OFF
```

### /monitor-hooks activate

```
All hooks activated.

Monitor: RUNNING (PID 12345, port 8080, http://localhost:8080)

Hook Configuration (/home/user/claude-hooks-monitor/hooks/hook_monitor.conf):
  SessionStart           ON
  UserPromptSubmit       ON
  PreToolUse             ON
  PostToolUse            ON
  ...
```

### /monitor-hooks show-all

```
Hook Audit (known hooks vs /home/user/claude-hooks-monitor/hooks/hook_monitor.conf):

  SessionStart           ON
  SessionEnd             ON
  UserPromptSubmit       ON
  PreToolUse             OFF
  PostToolUse            OFF
  PostToolUseFailure     ON
  PermissionRequest      ON
  Notification           ON
  SubagentStart          ON
  SubagentStop           ON
  Stop                   ON
  TeammateIdle           ON
  TaskCompleted          ON
  ConfigChange           ON
  PreCompact             ON

Summary: 15 known hooks, 15 in config, 0 missing, 0 extra
```

### /monitor-hooks PreToolUse on

```
PreToolUse = yes (enabled)
```

### /monitor-hooks help

```
Usage: /monitor-hooks <subcommand>

Subcommands:
  activate              Enable all hooks
  deactivate            Disable all hooks
  status                Show monitor state + per-hook config
  show-all              Audit: known hooks vs config (find missing/extra)
  <HookType> on         Enable a specific hook
  <HookType> off        Disable a specific hook

Valid hook types:
  SessionStart  SessionEnd  UserPromptSubmit  PreToolUse
  PostToolUse  PostToolUseFailure  PermissionRequest
  Notification  SubagentStart  SubagentStop  Stop
  TeammateIdle  TaskCompleted  ConfigChange  PreCompact

Examples:
  /monitor-hooks activate
  /monitor-hooks PreToolUse off
  /monitor-hooks status
```
