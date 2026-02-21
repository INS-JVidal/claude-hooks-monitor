# /monitor-hooks — Toggle hook monitoring

Control which Claude Code hooks are sent to the monitor. Changes take effect immediately.

**Usage:** `/monitor-hooks <subcommand>`

| Command | Effect |
|---------|--------|
| `activate` | Enable all hooks |
| `deactivate` | Disable all hooks |
| `status` | Show monitor state + per-hook config |
| `<HookType> on` | Enable a specific hook (e.g., `PreToolUse on`) |
| `<HookType> off` | Disable a specific hook |
| `show-all` | Audit: compare known hooks vs config file (find missing/extra) |

---

Run the following script. Use `$ARGUMENTS` for the subcommand passed by the user.

```bash
#!/usr/bin/env bash
set -euo pipefail

# ── Paths ─────────────────────────────────────────────────────────────
MONITOR_DIR=$(cd "$CLAUDE_PROJECT_DIR" 2>/dev/null && pwd) || {
    echo "Error: CLAUDE_PROJECT_DIR does not exist: $CLAUDE_PROJECT_DIR" >&2
    exit 1
}
CONF="$MONITOR_DIR/hooks/hook_monitor.conf"
LOCK_FILE="$MONITOR_DIR/hooks/.monitor-lock"
PORT_FILE="$MONITOR_DIR/hooks/.monitor-port"

# All 15 valid hook type names (must match config file keys exactly)
VALID_HOOKS="SessionStart SessionEnd UserPromptSubmit PreToolUse PostToolUse PostToolUseFailure PermissionRequest Notification SubagentStart SubagentStop Stop TeammateIdle TaskCompleted ConfigChange PreCompact"

# ── Globals for function communication ────────────────────────────────
# (Claude Code's template engine replaces $1/$2, so we use globals instead)
_RESOLVE_INPUT=""
_RESOLVE_RESULT=""
_HOOK_NAME=""
_HOOK_VAL=""
_MONITOR_PID=""

# ── Helpers ───────────────────────────────────────────────────────────

# Resolve a case-insensitive hook name to the canonical form.
# Sets _RESOLVE_RESULT to the canonical name, or empty if no match.
# Input: _RESOLVE_INPUT
resolve_hook_name() {
    _RESOLVE_RESULT=""
    local input_lower
    input_lower=$(echo "$_RESOLVE_INPUT" | tr '[:upper:]' '[:lower:]')
    for hook in $VALID_HOOKS; do
        local hook_lower
        hook_lower=$(echo "$hook" | tr '[:upper:]' '[:lower:]')
        if [ "$input_lower" = "$hook_lower" ]; then
            _RESOLVE_RESULT="$hook"
            return 0
        fi
    done
    return 1
}

# Check if the monitor process is running.
# Sets _MONITOR_PID if running. Returns 0 if running, 1 if not.
# [BUG-07 fix] Validates PID is numeric and verifies process identity via /proc.
# [BUG-17 fix] Rejects non-numeric lock file content.
is_monitor_running() {
    _MONITOR_PID=""
    if [ -f "$LOCK_FILE" ]; then
        local pid
        pid=$(tr -d '[:space:]' < "$LOCK_FILE" 2>/dev/null) || true
        # Validate PID is numeric
        case "$pid" in
            ''|*[!0-9]*)
                return 1
                ;;
        esac
        if kill -0 "$pid" 2>/dev/null; then
            # Verify the process is actually the monitor (not a recycled PID)
            if [ -d "/proc/$pid" ]; then
                local cmdline
                cmdline=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null) || true
                if echo "$cmdline" | grep -q "hook_monitor" 2>/dev/null; then
                    _MONITOR_PID="$pid"
                    return 0
                fi
                # /proc check inconclusive (e.g., non-Linux) — fall back to kill -0
                if [ ! -f "/proc/$pid/cmdline" ]; then
                    _MONITOR_PID="$pid"
                    return 0
                fi
                # PID exists but is not the monitor process
                return 1
            else
                # No /proc filesystem — trust kill -0 (non-Linux)
                _MONITOR_PID="$pid"
                return 0
            fi
        fi
    fi
    return 1
}

# [BUG-06 fix] Validate port is numeric and non-empty.
get_port() {
    local port=""
    if [ -f "$PORT_FILE" ]; then
        port=$(tr -d '[:space:]' < "$PORT_FILE" 2>/dev/null) || true
    fi
    case "$port" in
        ''|*[!0-9]*)
            echo ""
            return 1
            ;;
    esac
    echo "$port"
}

show_status() {
    # Monitor state
    if is_monitor_running; then
        local port
        port=$(get_port) || true
        if [ -n "$port" ]; then
            echo "Monitor: RUNNING (PID $_MONITOR_PID, port $port, http://localhost:$port)"
        else
            echo "Monitor: RUNNING (PID $_MONITOR_PID, port unknown)"
        fi
    else
        echo "Monitor: STOPPED"
    fi
    echo ""

    # Hook config
    if [ ! -f "$CONF" ]; then
        echo "Error: Config file not found: $CONF"
        return 0
    fi

    echo "Hook Configuration ($CONF):"
    local in_section=false
    local found_section=false
    local trimmed
    while IFS= read -r line; do
        trimmed=$(echo "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        # Skip blanks and comments
        [ -z "$trimmed" ] && continue
        echo "$trimmed" | grep -q '^#' && continue
        # Section header
        if echo "$trimmed" | grep -q '^\['; then
            if echo "$trimmed" | grep -iq '^\[hooks\]'; then
                in_section=true
                found_section=true
            else
                in_section=false
            fi
            continue
        fi
        $in_section || continue
        # Parse "Key = value" — skip malformed lines without '='
        case "$trimmed" in
            *=*) ;;
            *)   continue ;;
        esac
        local key val
        key=$(echo "$trimmed" | cut -d= -f1 | sed 's/[[:space:]]*$//')
        val=$(echo "$trimmed" | cut -d= -f2 | sed 's/^[[:space:]]*//' | tr '[:upper:]' '[:lower:]')
        if [ "$val" = "yes" ]; then
            printf "  %-22s ON\n" "$key"
        else
            printf "  %-22s OFF\n" "$key"
        fi
    done < "$CONF"

    # [BUG-08 fix] Warn if no [hooks] section was found
    if ! $found_section; then
        echo "  Warning: No [hooks] section found in config file."
        echo "  Expected a section like: [hooks]"
    fi
}

show_all() {
    # Audit: compare VALID_HOOKS against config file entries.
    # Shows missing hooks, extra hooks, and current state of each.
    if [ ! -f "$CONF" ]; then
        echo "Error: Config file not found: $CONF"
        return 1
    fi

    # 1. Parse config into entries: "HookName=yes" per line
    local config_entries=""
    local in_section=false
    local found_section=false
    local trimmed
    while IFS= read -r line; do
        trimmed=$(echo "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        [ -z "$trimmed" ] && continue
        echo "$trimmed" | grep -q '^#' && continue
        if echo "$trimmed" | grep -q '^\['; then
            if echo "$trimmed" | grep -iq '^\[hooks\]'; then
                in_section=true
                found_section=true
            else
                in_section=false
            fi
            continue
        fi
        $in_section || continue
        # [BUG-10 fix] Skip malformed lines without '='
        case "$trimmed" in
            *=*) ;;
            *)   continue ;;
        esac
        local key val
        key=$(echo "$trimmed" | cut -d= -f1 | sed 's/[[:space:]]*$//')
        val=$(echo "$trimmed" | cut -d= -f2 | sed 's/^[[:space:]]*//' | tr '[:upper:]' '[:lower:]')
        config_entries="${config_entries}${key}=${val}"$'\n'
    done < "$CONF"

    if ! $found_section; then
        echo "Error: No [hooks] section found in $CONF"
        return 1
    fi

    # 2. Show each known hook with its config status
    echo "Hook Audit (known hooks vs $CONF):"
    echo ""
    local missing_count=0
    for hook in $VALID_HOOKS; do
        local entry
        entry=$(echo "$config_entries" | grep "^${hook}=" || true)
        if [ -n "$entry" ]; then
            local val
            val=$(echo "$entry" | cut -d= -f2)
            if [ "$val" = "yes" ]; then
                printf "  %-22s ON\n" "$hook"
            else
                printf "  %-22s OFF\n" "$hook"
            fi
        else
            printf "  %-22s --    (MISSING from config, defaults to ON)\n" "$hook"
            missing_count=$((missing_count + 1))
        fi
    done

    # 3. Check for extra entries in config not in VALID_HOOKS
    # [BUG-12 fix] Use case-insensitive comparison for extra-entry detection
    echo ""
    local extra_count=0
    while IFS= read -r entry; do
        [ -z "$entry" ] && continue
        local key
        key=$(echo "$entry" | cut -d= -f1)
        local key_lower
        key_lower=$(echo "$key" | tr '[:upper:]' '[:lower:]')
        local found=false
        for hook in $VALID_HOOKS; do
            local hook_lower
            hook_lower=$(echo "$hook" | tr '[:upper:]' '[:lower:]')
            if [ "$key_lower" = "$hook_lower" ]; then
                found=true
                # Flag if casing doesn't match exactly
                if [ "$key" != "$hook" ]; then
                    if [ "$extra_count" -eq 0 ]; then
                        echo "Warnings:"
                    fi
                    printf "  %-22s (wrong case — expected '%s', found '%s')\n" "$key" "$hook" "$key"
                    extra_count=$((extra_count + 1))
                fi
                break
            fi
        done
        if ! $found; then
            if [ "$extra_count" -eq 0 ]; then
                echo "Extra entries in config (not in known hooks list):"
            fi
            printf "  %-22s (unknown hook type)\n" "$key"
            extra_count=$((extra_count + 1))
        fi
    done <<EOF
$config_entries
EOF

    # 4. Summary
    echo ""
    # [BUG-16 fix] Use named variable instead of _
    local known_count=0
    for hook in $VALID_HOOKS; do known_count=$((known_count + 1)); done
    local config_count
    config_count=$(printf '%s' "$config_entries" | grep -c '=' || true)
    echo "Summary: $known_count known hooks, $config_count in config, $missing_count missing, $extra_count extra"
}

# Portable sed in-place edit that preserves file permissions.
# [BUG-01 fix]  Proper error handling — propagates failures, always cleans up.
# [BUG-14 fix]  Preserves original file permissions using cp --attributes-only or chmod.
# Input: _SED_EXPR (sed expression), _SED_FILE (target file)
sed_inplace() {
    local tmp_file="${_SED_FILE}.tmp.$$"

    # Capture original permissions before modifying anything
    local orig_perms
    orig_perms=$(stat -c '%a' "$_SED_FILE" 2>/dev/null) || orig_perms="644"

    # Run sed, capturing errors
    if ! sed "$_SED_EXPR" "$_SED_FILE" > "$tmp_file" 2>/dev/null; then
        rm -f "$tmp_file"
        echo "Error: Failed to process config file: $_SED_FILE" >&2
        return 1
    fi

    # Verify the output is not empty (guard against disk-full / truncation)
    if [ ! -s "$tmp_file" ] && [ -s "$_SED_FILE" ]; then
        rm -f "$tmp_file"
        echo "Error: Config write produced empty output — original preserved." >&2
        return 1
    fi

    # Restore original permissions on the tmp file before atomic mv
    chmod "$orig_perms" "$tmp_file" 2>/dev/null || true

    # Atomic rename
    if ! mv "$tmp_file" "$_SED_FILE"; then
        rm -f "$tmp_file"
        echo "Error: Failed to write config file: $_SED_FILE" >&2
        return 1
    fi
}

# Set a hook to a value in the config file.
# [BUG-02 fix] Verifies the hook key exists before attempting substitution.
# Input: _HOOK_NAME, _HOOK_VAL ("yes" or "no")
set_hook() {
    if [ ! -f "$CONF" ]; then
        echo "Error: Config file not found: $CONF" >&2
        return 1
    fi
    # Verify the key exists in the config
    if ! grep -q "^[[:space:]]*${_HOOK_NAME}[[:space:]]*=" "$CONF"; then
        echo "Error: Hook '${_HOOK_NAME}' not found in config file. Add it manually: ${_HOOK_NAME} = ${_HOOK_VAL}" >&2
        return 1
    fi
    _SED_EXPR="s/^\([[:space:]]*${_HOOK_NAME}[[:space:]]*=[[:space:]]*\).*/\1${_HOOK_VAL}/"
    _SED_FILE="$CONF"
    sed_inplace
}

# Set all hooks to a value in a single atomic write.
# [BUG-01/03/11 fix] Builds one sed expression for all hooks, writes once.
# Returns the number of hooks that were NOT found in the config.
set_all_hooks() {
    local target_val="$_HOOK_VAL"
    if [ ! -f "$CONF" ]; then
        echo "Error: Config file not found: $CONF" >&2
        return 1
    fi

    local sed_expr=""
    local missing_hooks=""
    local missing_count=0

    for hook in $VALID_HOOKS; do
        if grep -q "^[[:space:]]*${hook}[[:space:]]*=" "$CONF"; then
            if [ -n "$sed_expr" ]; then
                sed_expr="${sed_expr};"
            fi
            sed_expr="${sed_expr}s/^\([[:space:]]*${hook}[[:space:]]*=[[:space:]]*\).*/\1${target_val}/"
        else
            missing_hooks="${missing_hooks}  ${hook}\n"
            missing_count=$((missing_count + 1))
        fi
    done

    if [ -z "$sed_expr" ]; then
        echo "Error: No hooks found in config file. Is [hooks] section present?" >&2
        return 1
    fi

    _SED_EXPR="$sed_expr"
    _SED_FILE="$CONF"
    if ! sed_inplace; then
        echo "Error: Failed to update config file." >&2
        return 1
    fi

    if [ "$missing_count" -gt 0 ]; then
        echo "Warning: $missing_count hook(s) not found in config (skipped):"
        printf '%b' "$missing_hooks"
    fi
    return 0
}

show_help() {
    echo "Usage: /monitor-hooks <subcommand>"
    echo ""
    echo "Subcommands:"
    echo "  activate              Enable all hooks"
    echo "  deactivate            Disable all hooks"
    echo "  status                Show monitor state + per-hook config"
    echo "  show-all              Audit: known hooks vs config (find missing/extra)"
    echo "  <HookType> on         Enable a specific hook"
    echo "  <HookType> off        Disable a specific hook"
    echo ""
    echo "Valid hook types:"
    echo "  SessionStart  SessionEnd  UserPromptSubmit  PreToolUse"
    echo "  PostToolUse  PostToolUseFailure  PermissionRequest"
    echo "  Notification  SubagentStart  SubagentStop  Stop"
    echo "  TeammateIdle  TaskCompleted  ConfigChange  PreCompact"
    echo ""
    echo "Examples:"
    echo "  /monitor-hooks activate"
    echo "  /monitor-hooks PreToolUse off"
    echo "  /monitor-hooks status"
}

# ── Parse arguments ───────────────────────────────────────────────────
# [BUG-04 fix] Default ARGUMENTS to empty string if unset
ARGUMENTS="${ARGUMENTS:-}"

# [BUG-13 fix] Parse only first two words — ignore trailing tokens
SUBCMD="${ARGUMENTS%% *}"
REST="${ARGUMENTS#* }"
if [ "$REST" = "$ARGUMENTS" ]; then
    REST=""
fi
# Extract only the first word of REST
REST="${REST%% *}"

case "$SUBCMD" in
    activate)
        _HOOK_VAL="yes"
        if set_all_hooks; then
            echo "All hooks activated."
        else
            echo ""
            echo "Some hooks could not be activated (see warnings above)."
        fi
        echo ""
        show_status
        ;;

    deactivate)
        _HOOK_VAL="no"
        if set_all_hooks; then
            echo "All hooks deactivated."
        else
            echo ""
            echo "Some hooks could not be deactivated (see warnings above)."
        fi
        echo ""
        show_status
        ;;

    status)
        show_status
        ;;

    show-all)
        show_all
        ;;

    ""|help)
        show_help
        ;;

    *)
        # Try to interpret as "<HookType> on/off"
        _RESOLVE_INPUT="$SUBCMD"
        resolve_hook_name || true
        if [ -z "$_RESOLVE_RESULT" ]; then
            echo "Unknown subcommand or hook type: $SUBCMD"
            echo ""
            show_help
            exit 1
        fi

        case "$REST" in
            on)
                _HOOK_NAME="$_RESOLVE_RESULT"
                _HOOK_VAL="yes"
                if set_hook; then
                    echo "$_RESOLVE_RESULT = yes (enabled)"
                else
                    exit 1
                fi
                ;;
            off)
                _HOOK_NAME="$_RESOLVE_RESULT"
                _HOOK_VAL="no"
                if set_hook; then
                    echo "$_RESOLVE_RESULT = no (disabled)"
                else
                    exit 1
                fi
                ;;
            "")
                echo "Missing 'on' or 'off' after hook type."
                echo ""
                echo "Usage: /monitor-hooks $_RESOLVE_RESULT on"
                echo "       /monitor-hooks $_RESOLVE_RESULT off"
                exit 1
                ;;
            *)
                echo "Expected 'on' or 'off' after hook type, got: '$REST'"
                echo ""
                echo "Usage: /monitor-hooks $_RESOLVE_RESULT on"
                echo "       /monitor-hooks $_RESOLVE_RESULT off"
                exit 1
                ;;
        esac
        ;;
esac
```
