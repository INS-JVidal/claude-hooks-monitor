#!/usr/bin/env bash
# test-hooks.sh — Test all hook types against the monitor server.
#
# Phase 1: Direct curl → server (tests all 15 hook endpoints)
# Phase 2: End-to-end stdin → hook-client (Go) → server
# Phase 3: Config toggle — verify disabling a hook skips it
set -euo pipefail

PORT="${PORT:-8080}"
BASE="http://localhost:${PORT}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK_CLIENT="${SCRIPT_DIR}/hooks/hook-client"
HOOK_CONF="${SCRIPT_DIR}/hooks/hook_monitor.conf"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

pass_count=0
fail_count=0

ok()   { pass_count=$((pass_count + 1)); echo -e "  ${GREEN}✓${NC} $1"; }
fail() { fail_count=$((fail_count + 1)); echo -e "  ${RED}✗${NC} $1"; }
info() { echo -e "  ${YELLOW}→${NC} $1"; }

send_hook() {
    local hook_type="$1"
    local payload="$2"
    info "Testing ${hook_type}..."
    local status
    status=$(curl -s -o /dev/null -w '%{http_code}' \
        -X POST "${BASE}/hook/${hook_type}" \
        -H "Content-Type: application/json" \
        -d "${payload}")
    if [ "$status" = "200" ]; then
        ok "${hook_type}"
    else
        fail "${hook_type} (HTTP ${status})"
    fi
    sleep 0.3
}

# =========================================================================
# Pre-flight check
# =========================================================================
echo ""
echo -e "${CYAN}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║       Claude Code Hooks Monitor — Test Suite            ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════════════╝${NC}"
echo ""

info "Checking server health at ${BASE}..."
if ! curl -sf "${BASE}/health" > /dev/null 2>&1; then
    echo -e "  ${RED}✗ Server is not running at ${BASE}${NC}"
    echo -e "  ${YELLOW}  Start it first: make run${NC}"
    exit 1
fi
ok "Server is healthy"
echo ""

# Common fields used in all payloads
SESSION_ID="test-session-001"
TRANSCRIPT="/home/test/.claude/projects/demo/transcript.jsonl"
CWD="/home/test/projects/demo"

# =========================================================================
# Phase 1: Direct server test — curl → server
# =========================================================================
echo -e "${CYAN}─── Phase 1: Direct server test (curl → server) ───${NC}"
echo ""

send_hook "SessionStart" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"SessionStart"
}'

send_hook "UserPromptSubmit" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"UserPromptSubmit",
  "prompt":"Create a Python script that calculates fibonacci numbers"
}'

send_hook "PreToolUse" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PreToolUse",
  "tool_name":"Write","tool_input":{"file_path":"fibonacci.py","content":"def fib(n): ..."}
}'

send_hook "PostToolUse" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PostToolUse",
  "tool_name":"Write","tool_input":{"file_path":"fibonacci.py"},
  "tool_response":{"success":true,"bytes_written":128}
}'

send_hook "PostToolUseFailure" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PostToolUseFailure",
  "tool_name":"Bash","tool_input":{"command":"python nonexistent.py"},
  "tool_response":{"exit_code":1,"stdout":"","stderr":"No such file"}
}'

send_hook "PreToolUse" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PreToolUse",
  "tool_name":"Bash","tool_input":{"command":"python fibonacci.py"}
}'

send_hook "PostToolUse" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PostToolUse",
  "tool_name":"Bash","tool_input":{"command":"python fibonacci.py"},
  "tool_response":{"exit_code":0,"stdout":"0 1 1 2 3 5 8 13 21 34","stderr":""}
}'

send_hook "Notification" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"Notification",
  "notification_type":"permission_prompt","message":"Claude needs permission to use Bash","tool_name":"Bash"
}'

send_hook "PermissionRequest" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PermissionRequest",
  "tool_name":"Bash","tool_input":{"command":"rm -rf test.txt"}
}'

send_hook "Stop" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"Stop",
  "stop_hook_active":true,"last_assistant_message":"Done with fibonacci."
}'

send_hook "SubagentStart" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"SubagentStart",
  "agent_id":"linter-agent-001","agent_type":"code-reviewer"
}'

send_hook "SubagentStop" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"SubagentStop",
  "agent_id":"linter-agent-001","agent_type":"code-reviewer",
  "agent_transcript_path":"/home/test/.claude/projects/demo/agents/linter.jsonl",
  "last_assistant_message":"Code quality check passed.","stop_hook_active":false
}'

send_hook "TeammateIdle" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"TeammateIdle"
}'

send_hook "TaskCompleted" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"TaskCompleted"
}'

send_hook "ConfigChange" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"ConfigChange"
}'

send_hook "PreCompact" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"PreCompact"
}'

send_hook "SessionEnd" '{
  "session_id":"'"$SESSION_ID"'","transcript_path":"'"$TRANSCRIPT"'",
  "cwd":"'"$CWD"'","permission_mode":"default","hook_event_name":"SessionEnd",
  "reason":"user_exit"
}'

echo ""

# =========================================================================
# Phase 2: End-to-end test — stdin → hook-client (Go) → server
# =========================================================================
echo -e "${CYAN}─── Phase 2: End-to-end test (hook-client → server) ───${NC}"
echo ""

if [ ! -x "$HOOK_CLIENT" ]; then
    fail "hook-client is not executable at ${HOOK_CLIENT}"
else
    # Get current event count before e2e test
    count_before=$(curl -sf "${BASE}/stats" | python3 -c "import sys,json; print(json.load(sys.stdin).get('total_hooks',0))" 2>/dev/null || echo 0)

    # Pipe a test payload through the Go hook-client
    info "Piping test payload through hook-client..."
    export HOOK_MONITOR_URL="${BASE}"
    stdout_bytes=$(echo '{"hook_event_name":"PreToolUse","session_id":"e2e-test","cwd":"/tmp","permission_mode":"default","tool_name":"Bash","tool_input":{"command":"echo e2e"}}' \
        | "$HOOK_CLIENT" | wc -c)

    sleep 0.5

    # Verify no stdout
    if [ "$stdout_bytes" -eq 0 ]; then
        ok "hook-client produced no stdout"
    else
        fail "hook-client produced ${stdout_bytes} bytes on stdout"
    fi

    # Verify event reached server
    count_after=$(curl -sf "${BASE}/stats" | python3 -c "import sys,json; print(json.load(sys.stdin).get('total_hooks',0))" 2>/dev/null || echo 0)
    if [ "$count_after" -gt "$count_before" ]; then
        ok "Event reached server via hook-client"
    else
        fail "Event did not reach server (before=${count_before}, after=${count_after})"
    fi
fi
echo ""

# =========================================================================
# Phase 3: Config toggle test
# =========================================================================
echo -e "${CYAN}─── Phase 3: Config toggle test ───${NC}"
echo ""

if [ ! -f "$HOOK_CONF" ]; then
    fail "hook_monitor.conf not found at ${HOOK_CONF}"
else
    # Save original config
    cp "$HOOK_CONF" "${HOOK_CONF}.bak"

    # Disable PreToolUse
    sed -i 's/^PreToolUse = yes/PreToolUse = no/' "$HOOK_CONF"
    info "Disabled PreToolUse in config"

    # Get current PreToolUse count
    pt_before=$(curl -sf "${BASE}/stats" | python3 -c "
import sys, json
stats = json.load(sys.stdin).get('stats', {})
print(stats.get('PreToolUse', 0))
" 2>/dev/null || echo 0)

    # Send a PreToolUse event through the hook-client (should be skipped)
    echo '{"hook_event_name":"PreToolUse","session_id":"toggle-test","cwd":"/tmp","permission_mode":"default","tool_name":"Test","tool_input":{"cmd":"noop"}}' \
        | HOOK_MONITOR_URL="${BASE}" "$HOOK_CLIENT" 2>/dev/null

    sleep 0.5

    # Check count didn't change
    pt_after=$(curl -sf "${BASE}/stats" | python3 -c "
import sys, json
stats = json.load(sys.stdin).get('stats', {})
print(stats.get('PreToolUse', 0))
" 2>/dev/null || echo 0)

    if [ "$pt_after" -eq "$pt_before" ]; then
        ok "Disabled hook was correctly skipped (PreToolUse count unchanged: ${pt_before})"
    else
        fail "Disabled hook was NOT skipped (before=${pt_before}, after=${pt_after})"
    fi

    # Restore config
    mv "${HOOK_CONF}.bak" "$HOOK_CONF"
    ok "Config restored to original"
fi
echo ""

# =========================================================================
# Summary
# =========================================================================
total=$((pass_count + fail_count))
echo -e "${CYAN}═══════════════════════════════════════════════════════${NC}"
if [ "$fail_count" -eq 0 ]; then
    echo -e "  ${GREEN}All ${total} tests passed!${NC}"
else
    echo -e "  ${GREEN}${pass_count} passed${NC}, ${RED}${fail_count} failed${NC} out of ${total}"
fi
echo -e "${CYAN}═══════════════════════════════════════════════════════${NC}"
echo ""
echo "  Check results:"
echo "    curl ${BASE}/stats | python3 -m json.tool"
echo "    curl ${BASE}/events?limit=5 | python3 -m json.tool"
echo ""

exit "$fail_count"
