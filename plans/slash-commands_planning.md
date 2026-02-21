# Hook Monitor Slash Commands - User Controlled Only

**Design Philosophy:** Students have 100% control. Monitor NEVER starts automatically.

---

## Quick Implementation

### Directory Structure

```
claude-hooks-monitor/
├── .claude/
│   └── commands/              # ⭐ Slash commands here
│       ├── monitor.md         # Main command
│       └── monitor-help.md    # Help/usage
├── bin/
│   └── monitor                # Go binary
├── hooks/
│   └── hook_monitor.sh        # Sends events to server
└── logs/
    └── monitor.log            # Server logs
```

---

## Single Command: `/monitor`

**File:** `.claude/commands/monitor.md`

```markdown
---
description: Control the Claude Code hook monitoring system
---

Interactive control for the hook monitoring server.

Usage:
  /monitor start     - Start the monitoring server
  /monitor stop      - Stop the monitoring server
  /monitor restart   - Restart the server
  /monitor status    - Check if running
  /monitor logs      - View recent logs
  /monitor ui        - Open dashboard in browser

Without arguments, shows current status and available commands.

```bash
#!/bin/bash

set -euo pipefail

cd "$CLAUDE_PROJECT_DIR"

# Configuration
MONITOR_BIN="./bin/monitor"
PID_FILE=".monitor.pid"
PORT_FILE="hooks/.monitor-port"
LOG_FILE="logs/monitor.log"

# Color codes (if terminal supports it)
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
is_running() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            return 0
        else
            # Stale PID file
            rm -f "$PID_FILE"
        fi
    fi
    return 1
}

get_port() {
    if [ -f "$PORT_FILE" ]; then
        cat "$PORT_FILE"
    else
        echo "unknown"
    fi
}

show_status() {
    echo "═══════════════════════════════════════════════════════"
    echo "  Claude Code Hook Monitor"
    echo "═══════════════════════════════════════════════════════"
    echo ""
    
    if is_running; then
        PID=$(cat "$PID_FILE")
        PORT=$(get_port)
        UPTIME=$(ps -p "$PID" -o etime= 2>/dev/null | xargs || echo "unknown")
        
        echo -e "${GREEN}Status: RUNNING${NC}"
        echo "  PID: $PID"
        echo "  Port: $PORT"
        echo "  Uptime: $UPTIME"
        echo "  URL: http://localhost:$PORT"
    else
        echo -e "${RED}Status: STOPPED${NC}"
    fi
    
    echo ""
    echo "Commands:"
    echo "  /monitor start     - Start monitoring"
    echo "  /monitor stop      - Stop monitoring"
    echo "  /monitor restart   - Restart monitoring"
    echo "  /monitor status    - Show this status"
    echo "  /monitor logs      - View recent logs"
    echo "  /monitor ui        - Open dashboard in browser"
    echo ""
}

start_monitor() {
    if is_running; then
        PORT=$(get_port)
        echo -e "${YELLOW}✓ Monitor is already running${NC}"
        echo "  URL: http://localhost:$PORT"
        return 0
    fi
    
    # Check if binary exists
    if [ ! -x "$MONITOR_BIN" ]; then
        echo -e "${RED}✗ Monitor binary not found or not executable${NC}"
        echo "  Expected: $MONITOR_BIN"
        echo ""
        echo "Build it with: go build -o bin/monitor cmd/monitor/main.go"
        return 1
    fi
    
    # Create logs directory
    mkdir -p logs
    
    # Start in background
    echo -e "${BLUE}🚀 Starting hook monitor...${NC}"
    
    nohup "$MONITOR_BIN" > "$LOG_FILE" 2>&1 &
    PID=$!
    
    # Save PID
    echo "$PID" > "$PID_FILE"
    
    # Wait for it to start (up to 5 seconds)
    for i in {1..10}; do
        sleep 0.5
        if [ -f "$PORT_FILE" ]; then
            break
        fi
    done
    
    # Verify it started
    if is_running && [ -f "$PORT_FILE" ]; then
        PORT=$(get_port)
        echo -e "${GREEN}✓ Monitor started successfully${NC}"
        echo "  PID: $PID"
        echo "  Port: $PORT"
        echo "  URL: http://localhost:$PORT"
        echo "  Logs: $LOG_FILE"
        echo ""
        echo "Now when Claude Code uses hooks, events will be captured!"
    else
        rm -f "$PID_FILE"
        echo -e "${RED}✗ Failed to start monitor${NC}"
        echo ""
        echo "Check logs:"
        echo "  tail -f $LOG_FILE"
        return 1
    fi
}

stop_monitor() {
    if ! is_running; then
        echo -e "${YELLOW}ℹ️  Monitor is not running${NC}"
        return 0
    fi
    
    PID=$(cat "$PID_FILE")
    
    echo -e "${BLUE}🛑 Stopping monitor...${NC}"
    
    # Try graceful shutdown (SIGTERM)
    kill -TERM "$PID" 2>/dev/null || true
    
    # Wait up to 5 seconds
    for i in {1..10}; do
        if ! ps -p "$PID" > /dev/null 2>&1; then
            break
        fi
        sleep 0.5
    done
    
    # Force kill if still running
    if ps -p "$PID" > /dev/null 2>&1; then
        echo -e "${YELLOW}⚠️  Graceful shutdown timeout, forcing...${NC}"
        kill -9 "$PID" 2>/dev/null || true
        sleep 1
    fi
    
    # Clean up
    rm -f "$PID_FILE"
    rm -f "$PORT_FILE"
    
    if ! ps -p "$PID" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Monitor stopped${NC}"
        echo "  Logs saved: $LOG_FILE"
    else
        echo -e "${RED}✗ Failed to stop monitor${NC}"
        return 1
    fi
}

restart_monitor() {
    echo -e "${BLUE}🔄 Restarting monitor...${NC}"
    stop_monitor
    sleep 1
    start_monitor
}

show_logs() {
    if [ ! -f "$LOG_FILE" ]; then
        echo -e "${YELLOW}ℹ️  No log file found${NC}"
        echo "  Start monitoring first: /monitor start"
        return 0
    fi
    
    echo "═══════════════════════════════════════════════════════"
    echo "  Monitor Logs (last 30 lines)"
    echo "═══════════════════════════════════════════════════════"
    echo ""
    tail -n 30 "$LOG_FILE"
    echo ""
    echo "View full log: cat $LOG_FILE"
    echo "Follow live: tail -f $LOG_FILE"
}

open_ui() {
    if ! is_running; then
        echo -e "${RED}✗ Monitor is not running${NC}"
        echo ""
        echo "Start it first: /monitor start"
        return 1
    fi
    
    PORT=$(get_port)
    URL="http://localhost:$PORT"
    
    echo -e "${BLUE}🌐 Opening dashboard...${NC}"
    echo "  URL: $URL"
    
    # Open browser (platform-specific)
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        open "$URL" 2>/dev/null || echo "  Please open manually: $URL"
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        if command -v xdg-open &> /dev/null; then
            xdg-open "$URL" 2>/dev/null || echo "  Please open manually: $URL"
        else
            echo "  Please open in browser: $URL"
        fi
    else
        echo "  Please open in browser: $URL"
    fi
}

# Main command logic
ACTION="${ARGUMENTS:-}"

case "$ACTION" in
    start)
        start_monitor
        ;;
    stop)
        stop_monitor
        ;;
    restart)
        restart_monitor
        ;;
    status|"")
        show_status
        ;;
    logs|log)
        show_logs
        ;;
    ui|dashboard|web)
        open_ui
        ;;
    *)
        echo -e "${RED}Unknown command: $ACTION${NC}"
        echo ""
        show_status
        exit 1
        ;;
esac
```
```

---

## Help Command (Optional)

**File:** `.claude/commands/monitor-help.md`

```markdown
---
description: Show detailed help for the hook monitoring system
---

Detailed help for using the Claude Code hook monitor.

```bash
#!/bin/bash

cat << 'EOF'
═══════════════════════════════════════════════════════
  Claude Code Hook Monitor - Help
═══════════════════════════════════════════════════════

WHAT IT DOES:
  Captures and visualizes all Claude Code hook events in real-time.
  Shows what Claude is doing: files edited, commands run, prompts
  submitted, etc.

BASIC USAGE:
  /monitor start     - Start the monitoring server
  /monitor stop      - Stop the monitoring server
  /monitor status    - Check if it's running
  /monitor ui        - Open the web dashboard

WORKFLOW:
  1. Start monitoring:
     > /monitor start
     
  2. Use Claude normally (write code, run commands, etc.)
     
  3. View events in real-time:
     > /monitor ui
     
  4. Stop when done:
     > /monitor stop

COMMANDS:
  /monitor start
    Starts the monitoring server in the background.
    Creates a web server on a random port (usually 40000+).
    All hook events will be captured and stored.
    
  /monitor stop
    Gracefully stops the monitoring server.
    Saves all logs before shutting down.
    
  /monitor restart
    Stops and starts the server (useful for updates).
    
  /monitor status
    Shows if server is running, port, uptime, etc.
    
  /monitor logs
    Shows the last 30 lines of the monitor log file.
    Useful for debugging if something goes wrong.
    
  /monitor ui
    Opens the web dashboard in your default browser.
    Only works if the monitor is running.

WHAT GETS MONITORED:
  ✓ User prompts you submit
  ✓ Tools Claude uses (Bash, Read, Write, Edit, etc.)
  ✓ Tool inputs and outputs
  ✓ Success/failure status
  ✓ Permissions requested
  ✓ Notifications sent
  ✓ Session start/stop
  ✓ Subagent creation/termination

FILES:
  .monitor.pid           - Process ID of running monitor
  hooks/.monitor-port    - Port number the server is using
  logs/monitor.log       - Server logs (errors, events)

TROUBLESHOOTING:
  Monitor won't start?
    - Check if binary exists: ls -l bin/monitor
    - Build it: go build -o bin/monitor cmd/monitor/main.go
    - Check logs: cat logs/monitor.log
    
  Can't connect to UI?
    - Check if running: /monitor status
    - Get the port: cat hooks/.monitor-port
    - Try manually: http://localhost:[port]
    
  Events not appearing?
    - Verify hooks are configured: cat .claude/settings.json
    - Check hook script exists: ls -l hooks/hook_monitor.sh
    - Make sure it's executable: chmod +x hooks/hook_monitor.sh
    
  Monitor using too much CPU/memory?
    - Stop it: /monitor stop
    - Check what's in logs: /monitor logs

EXAMPLE SESSION:
  > /monitor start
  🚀 Starting hook monitor...
  ✓ Monitor started successfully
    Port: 40941
    URL: http://localhost:40941
  
  > claude, write a hello world in Python
  [Claude writes hello.py]
  
  > /monitor ui
  🌐 Opening dashboard...
  [Browser opens showing all events]
  
  > /monitor stop
  🛑 Stopping monitor...
  ✓ Monitor stopped
  
For more information, see the README.md in the project root.
EOF
```
```

---

## Update Your Go Monitor for PID File

**File:** `cmd/monitor/main.go` (additions)

```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"
)

func main() {
    // Start server on random port
    port, err := startServer()
    if err != nil {
        log.Fatal(err)
    }
    
    // Save port to file (so slash command can find it)
    portFile := "hooks/.monitor-port"
    os.MkdirAll(filepath.Dir(portFile), 0755)
    if err := os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0644); err != nil {
        log.Printf("Warning: Could not write port file: %v", err)
    }
    
    // Save PID (so slash command can manage process)
    pidFile := ".monitor.pid"
    if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
        log.Printf("Warning: Could not write PID file: %v", err)
    }
    
    log.Printf("Monitor started on port %d", port)
    log.Printf("PID: %d", os.Getpid())
    
    // Handle graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        log.Println("Shutdown signal received, cleaning up...")
        
        // Clean up files
        os.Remove(portFile)
        os.Remove(pidFile)
        
        // TODO: Close server gracefully
        
        os.Exit(0)
    }()
    
    // Start HTTP server
    // ... your server code ...
}

func startServer() (int, error) {
    // Try to find available port starting at 40000
    for port := 40000; port < 50000; port++ {
        addr := fmt.Sprintf(":%d", port)
        listener, err := net.Listen("tcp", addr)
        if err == nil {
            // Port is available
            go http.Serve(listener, yourHandler)
            return port, nil
        }
    }
    return 0, fmt.Errorf("no available ports")
}
```

---

## Student Workflow

### First Time Setup

```bash
# Build the monitor
cd claude-hooks-monitor
go build -o bin/monitor cmd/monitor/main.go

# Make hook script executable
chmod +x hooks/hook_monitor.sh

# Configure hooks (already done in settings.json)
cat .claude/settings.json
```

### Daily Usage

```bash
# Start Claude Code
claude

# Want to see what Claude is doing?
> /monitor start
✓ Monitor started on http://localhost:40941

# Work with Claude normally
> write a Python script to sort a list

# View the events
> /monitor ui
[Browser opens with event tree]

# Done for the day
> /monitor stop
✓ Monitor stopped
```

---

## Key Features

### ✅ 100% User Controlled

- Monitor NEVER starts automatically
- Student decides when to monitor
- Clean start/stop workflow

### ✅ Simple Commands

```bash
/monitor start   # Start
/monitor stop    # Stop
/monitor status  # Check
/monitor ui      # View
```

### ✅ Robust Process Management

- PID file tracking
- Graceful shutdown (SIGTERM)
- Forced kill fallback (SIGKILL)
- Stale PID cleanup

### ✅ Clear Feedback

```bash
> /monitor start
🚀 Starting hook monitor...
✓ Monitor started successfully
  PID: 12345
  Port: 40941
  URL: http://localhost:40941
```

### ✅ Error Handling

```bash
> /monitor start
✗ Monitor binary not found
  Expected: ./bin/monitor
  
Build it with: go build -o bin/monitor cmd/monitor/main.go
```

---

## Testing Checklist

### ✓ Basic Operations
```bash
/monitor start    # Should start
/monitor status   # Should show running
/monitor stop     # Should stop
/monitor status   # Should show stopped
```

### ✓ Restart
```bash
/monitor start
/monitor restart  # Should stop then start
```

### ✓ Multiple Starts
```bash
/monitor start
/monitor start    # Should say "already running"
```

### ✓ View Logs
```bash
/monitor start
# Use Claude to trigger some events
/monitor logs     # Should show events
```

### ✓ Open UI
```bash
/monitor start
/monitor ui       # Should open browser
```

### ✓ Stop When Not Running
```bash
/monitor stop     # Should say "not running"
```

---

## Why This Approach?

### ❌ Auto-Start Problems:

- Students don't know it's running
- Wastes resources when not needed
- Harder to debug
- Students lose control

### ✅ Manual Control Benefits:

- **Intentional monitoring** - Students decide when
- **Resource efficient** - Only runs when needed
- **Educational** - Students learn process management
- **Debugging friendly** - Easy to restart/check logs
- **Transparent** - Students see what's happening

---

## Optional: Quick Start Alias

If students want a shortcut, they can add to their shell config:

```bash
# ~/.bashrc or ~/.zshrc
alias monitor-start='cd ~/projects/claude-hooks-monitor && claude -c "/monitor start"'
alias monitor-stop='cd ~/projects/claude-hooks-monitor && claude -c "/monitor stop"'
```

Then from anywhere:
```bash
$ monitor-start
$ monitor-stop
```

But within Claude Code, they use:
```bash
> /monitor start
> /monitor stop
```

---

## Summary

**Single command file:** `.claude/commands/monitor.md`

**User workflow:**
1. `/monitor start` - When they want to see events
2. Work with Claude normally
3. `/monitor ui` - View the dashboard
4. `/monitor stop` - When done

**Control:** 100% in student's hands  
**Auto-start:** NEVER  
**Transparency:** ALWAYS

Perfect for teaching! 🎓
