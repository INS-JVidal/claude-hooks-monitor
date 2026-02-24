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
# Priority: XDG config dir (system-wide install) → project-local (legacy).
XDG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/claude-hooks-monitor"
if [[ -d "$XDG_DIR" && -f "$XDG_DIR/hook_monitor.conf" ]]; then
    MONITOR_DIR="$XDG_DIR"
    # Project-level config override: check $CLAUDE_PROJECT_DIR/.claude/ first
    if [[ -n "${CLAUDE_PROJECT_DIR:-}" && -f "$CLAUDE_PROJECT_DIR/.claude/hook_monitor.conf" ]]; then
        CONF="$CLAUDE_PROJECT_DIR/.claude/hook_monitor.conf"
    else
        CONF="$MONITOR_DIR/hook_monitor.conf"
    fi
    LOCK_FILE="$MONITOR_DIR/.monitor-lock"
    PORT_FILE="$MONITOR_DIR/.monitor-port"
elif [[ -n "${CLAUDE_PROJECT_DIR:-}" ]]; then
    MONITOR_DIR=$(cd "$CLAUDE_PROJECT_DIR" 2>/dev/null && pwd) || {
        echo "Error: CLAUDE_PROJECT_DIR does not exist." >&2
        exit 1
    }
    CONF="$MONITOR_DIR/hooks/hook_monitor.conf"
    LOCK_FILE="$MONITOR_DIR/hooks/.monitor-lock"
    PORT_FILE="$MONITOR_DIR/hooks/.monitor-port"
else
    echo "Error: No monitor config found." >&2
    echo "Run 'make install' from the claude-hooks-monitor project." >&2
    exit 1
fi

# All 15 valid hook type names (must match config file keys exactly)
VALID_HOOKS=(
    SessionStart SessionEnd UserPromptSubmit PreToolUse PostToolUse
    PostToolUseFailure PermissionRequest Notification SubagentStart
    SubagentStop Stop TeammateIdle TaskCompleted ConfigChange PreCompact
)

# ── Parsed config (populated by parse_hooks_section) ─────────────────
declare -A HOOK_CFG           # hook_name → "yes"|"no"
declare -a HOOK_CFG_KEYS=()   # insertion-ordered keys from config
HOOKS_SECTION_FOUND=false

# ── Helpers ───────────────────────────────────────────────────────────

# Trim leading/trailing whitespace using bash builtins (no subshells).
trim() {
    local s="$1"
    s="${s#"${s%%[![:space:]]*}"}"
    s="${s%"${s##*[![:space:]]}"}"
    printf '%s' "$s"
}

# Parse the [hooks] section of the config file into HOOK_CFG associative array.
# Populates HOOK_CFG, HOOK_CFG_KEYS, and HOOKS_SECTION_FOUND.
# Returns 0 on success, 1 if config file not found.
parse_hooks_section() {
    HOOK_CFG=()
    HOOK_CFG_KEYS=()
    HOOKS_SECTION_FOUND=false

    if [[ ! -f "$CONF" ]]; then
        echo "Error: Config file not found: $CONF"
        return 1
    fi

    local in_section=false first_line=true
    while IFS= read -r line || [[ -n "$line" ]]; do
        # Strip UTF-8 BOM if present on the first line (Windows editors).
        if $first_line; then
            line="${line#$'\xef\xbb\xbf'}"
            first_line=false
        fi
        # Trim whitespace
        line=$(trim "$line")
        # Skip blanks and comments
        [[ -z "$line" || "$line" == \#* ]] && continue
        # Section header
        if [[ "$line" == \[* ]]; then
            if [[ "${line,,}" == "[hooks]" ]]; then
                in_section=true
                HOOKS_SECTION_FOUND=true
            else
                in_section=false
            fi
            continue
        fi
        $in_section || continue
        # Must contain '='
        [[ "$line" == *=* ]] || continue
        # Split on first '='
        local key val
        key=$(trim "${line%%=*}")
        val=$(trim "${line#*=}")
        # Strip inline comments: "yes # enable" → "yes"
        val="${val%%#*}"
        val=$(trim "$val")
        val="${val,,}"  # lowercase
        # Normalize key to canonical PascalCase if it matches a known hook
        # (case-insensitive) — this ensures show_all/set_hook lookups work
        # even if the user manually edits the config with wrong casing.
        local key_lower="${key,,}"
        for _vh in "${VALID_HOOKS[@]}"; do
            if [[ "${_vh,,}" == "$key_lower" ]]; then
                key="$_vh"
                break
            fi
        done
        # Deduplicate: skip if this canonical key was already seen
        if [[ -v "HOOK_CFG[$key]" ]]; then
            HOOK_CFG["$key"]="$val"
            continue
        fi
        HOOK_CFG["$key"]="$val"
        HOOK_CFG_KEYS+=("$key")
    done < "$CONF"
}

# Resolve a case-insensitive hook name to the canonical PascalCase form.
# Prints the canonical name on stdout. Returns 1 if no match.
resolve_hook_name() {
    local input="${1,,}"  # lowercase
    for hook in "${VALID_HOOKS[@]}"; do
        if [[ "${hook,,}" == "$input" ]]; then
            printf '%s' "$hook"
            return 0
        fi
    done
    return 1
}

# Check if the monitor process is running.
# Sets MONITOR_PID if running. Returns 0 if running, 1 if not.
is_monitor_running() {
    MONITOR_PID=""
    [[ -f "$LOCK_FILE" ]] || return 1

    local pid
    pid=$(<"$LOCK_FILE") 2>/dev/null || return 1
    pid="${pid//[[:space:]]/}"  # strip whitespace

    # Validate PID is numeric
    [[ "$pid" =~ ^[0-9]+$ ]] || return 1

    kill -0 "$pid" 2>/dev/null || return 1

    # Verify the process is actually the monitor (not a recycled PID)
    if [[ -d "/proc/$pid" ]]; then
        # Prefer /proc/pid/exe (exact binary path) over cmdline substring matching.
        local exe
        exe=$(readlink -f "/proc/$pid/exe" 2>/dev/null) || true
        if [[ -n "$exe" ]]; then
            local basename_exe
            basename_exe="${exe##*/}"
            if [[ "$basename_exe" == "monitor" || "$basename_exe" == "claude-hooks-monitor" ]]; then
                MONITOR_PID="$pid"
                return 0
            fi
            # PID exists but binary name doesn't match
            return 1
        fi
        # No /proc/pid/exe — fall back to cmdline check
        if [[ -f "/proc/$pid/cmdline" ]]; then
            local cmdline
            cmdline=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null) || true
            if [[ "$cmdline" == *claude-hooks-monitor* || "$cmdline" == *hook_monitor* ]]; then
                MONITOR_PID="$pid"
                return 0
            fi
            return 1
        fi
        # /proc exists but no exe or cmdline — trust kill -0
        MONITOR_PID="$pid"
        return 0
    fi
    # No /proc filesystem (non-Linux) — trust kill -0
    MONITOR_PID="$pid"
    return 0
}

# Read the port file. Prints port on stdout; returns 1 if invalid.
get_port() {
    [[ -f "$PORT_FILE" ]] || { echo ""; return 1; }
    local port
    port=$(<"$PORT_FILE") 2>/dev/null || { echo ""; return 1; }
    port="${port//[[:space:]]/}"
    [[ "$port" =~ ^[0-9]+$ ]] || { echo ""; return 1; }
    # Validate port is in valid TCP range.
    (( port >= 1 && port <= 65535 )) || { echo ""; return 1; }
    echo "$port"
}

show_status() {
    # Monitor state
    if is_monitor_running; then
        local port
        port=$(get_port) || true
        if [[ -n "$port" ]]; then
            echo "Monitor: RUNNING (PID $MONITOR_PID, port $port, http://localhost:$port)"
        else
            echo "Monitor: RUNNING (PID $MONITOR_PID, port unknown)"
        fi
    else
        echo "Monitor: STOPPED"
    fi
    echo ""

    # Parse config (shared function)
    parse_hooks_section || return 0

    if ! $HOOKS_SECTION_FOUND; then
        echo "  Warning: No [hooks] section found in config file."
        echo "  Expected a section like: [hooks]"
        return 0
    fi

    echo "Hook Configuration ($CONF):"
    for key in "${HOOK_CFG_KEYS[@]}"; do
        # Match Go semantics: only "no" disables; anything else (yes, true, 1, empty) = ON.
        if [[ "${HOOK_CFG[$key]}" == "no" ]]; then
            printf "  %-22s OFF\n" "$key"
        else
            printf "  %-22s ON\n" "$key"
        fi
    done
}

show_all() {
    # Parse config (shared function)
    parse_hooks_section || return 1

    if ! $HOOKS_SECTION_FOUND; then
        echo "Error: No [hooks] section found in $CONF"
        return 1
    fi

    echo "Hook Audit (known hooks vs $CONF):"
    echo ""
    local missing_count=0
    for hook in "${VALID_HOOKS[@]}"; do
        if [[ -v "HOOK_CFG[$hook]" ]]; then
            if [[ "${HOOK_CFG[$hook]}" == "no" ]]; then
                printf "  %-22s OFF\n" "$hook"
            else
                printf "  %-22s ON\n" "$hook"
            fi
        else
            printf "  %-22s --    (MISSING from config, defaults to ON)\n" "$hook"
            missing_count=$((missing_count + 1))
        fi
    done

    # Check for extra/misnamed entries in config
    echo ""
    local extra_count=0
    for key in "${HOOK_CFG_KEYS[@]}"; do
        local key_lower="${key,,}"
        local found=false
        for hook in "${VALID_HOOKS[@]}"; do
            if [[ "${hook,,}" == "$key_lower" ]]; then
                found=true
                # Flag casing mismatch
                if [[ "$key" != "$hook" ]]; then
                    if [[ "$extra_count" -eq 0 ]]; then
                        echo "Warnings:"
                    fi
                    printf "  %-22s (wrong case — expected '%s', found '%s')\n" "$key" "$hook" "$key"
                    extra_count=$((extra_count + 1))
                fi
                break
            fi
        done
        if ! $found; then
            if [[ "$extra_count" -eq 0 ]]; then
                echo "Extra entries in config (not in known hooks list):"
            fi
            printf "  %-22s (unknown hook type)\n" "$key"
            extra_count=$((extra_count + 1))
        fi
    done

    echo ""
    echo "Summary: ${#VALID_HOOKS[@]} known hooks, ${#HOOK_CFG_KEYS[@]} in config, $missing_count missing, $extra_count extra"
}

# Portable sed in-place edit that preserves file permissions.
# Arguments: sed_expr file
sed_inplace() {
    local sed_expr="$1" file="$2"
    local tmp_file
    tmp_file=$(mktemp "${file}.tmp.XXXXXX") || {
        echo "Error: could not create temp file for $file" >&2
        return 1
    }

    # Advisory lock prevents concurrent sed_inplace calls from racing
    # (e.g. two Claude sessions toggling hooks simultaneously).
    (
        flock -x 200 2>/dev/null || true  # best-effort — skip if flock unavailable

        # Capture original permissions
        local orig_perms
        orig_perms=$(stat -c '%a' "$file" 2>/dev/null) || orig_perms="644"

        if ! sed "$sed_expr" "$file" > "$tmp_file" 2>/dev/null; then
            rm -f "$tmp_file"
            echo "Error: Failed to process config file: $file" >&2
            return 1
        fi

        # Guard against empty output (disk-full / truncation)
        if [[ ! -s "$tmp_file" && -s "$file" ]]; then
            rm -f "$tmp_file"
            echo "Error: Config write produced empty output — original preserved." >&2
            return 1
        fi

        chmod "$orig_perms" "$tmp_file" 2>/dev/null || true

        if ! mv "$tmp_file" "$file"; then
            rm -f "$tmp_file"
            echo "Error: Failed to write config file: $file" >&2
            return 1
        fi
    ) 200>"${file}.lock"
    local rc=$?
    rm -f "${file}.lock"
    return $rc
}

# Set a single hook value in the config file.
# Arguments: hook_name value
set_hook() {
    local hook_name="$1" hook_val="$2"
    # Guard against regex injection — hook names must be alphanumeric PascalCase.
    if [[ ! "$hook_name" =~ ^[A-Za-z][A-Za-z0-9]*$ ]]; then
        echo "Error: Invalid hook name: $hook_name" >&2
        return 1
    fi
    if [[ ! -f "$CONF" ]]; then
        echo "Error: Config file not found: $CONF" >&2
        return 1
    fi
    if ! grep -q "^[[:space:]]*${hook_name}[[:space:]]*=" "$CONF"; then
        echo "Error: Hook '${hook_name}' not found in config file. Add it manually: ${hook_name} = ${hook_val}" >&2
        return 1
    fi
    sed_inplace "s/^\([[:space:]]*${hook_name}[[:space:]]*=[[:space:]]*\).*/\1${hook_val}/" "$CONF"
}

# Set all hooks to a value in a single atomic write.
# Reads config once, builds one compound sed expression, writes once.
# Arguments: target_value ("yes" or "no")
set_all_hooks() {
    local target_val="$1"
    if [[ ! -f "$CONF" ]]; then
        echo "Error: Config file not found: $CONF" >&2
        return 1
    fi

    # Read file once into memory
    local content
    content=$(<"$CONF")

    local sed_expr=""
    local missing_hooks=""
    local missing_count=0

    for hook in "${VALID_HOOKS[@]}"; do
        if [[ "$content" =~ ^[[:space:]]*${hook}[[:space:]]*= ]] || \
           grep -q "^[[:space:]]*${hook}[[:space:]]*=" <<< "$content"; then
            [[ -n "$sed_expr" ]] && sed_expr="${sed_expr};"
            sed_expr="${sed_expr}s/^\([[:space:]]*${hook}[[:space:]]*=[[:space:]]*\).*/\1${target_val}/"
        else
            missing_hooks+="  ${hook}\n"
            missing_count=$((missing_count + 1))
        fi
    done

    if [[ -z "$sed_expr" ]]; then
        echo "Error: No hooks found in config file. Is [hooks] section present?" >&2
        return 1
    fi

    if ! sed_inplace "$sed_expr" "$CONF"; then
        echo "Error: Failed to update config file." >&2
        return 1
    fi

    if [[ "$missing_count" -gt 0 ]]; then
        echo "Warning: $missing_count hook(s) not found in config (skipped):"
        printf '%b' "$missing_hooks"
    fi
    return 0
}

show_help() {
    cat <<'HELPEOF'
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
HELPEOF
}

# ── Parse arguments ───────────────────────────────────────────────────
ARGUMENTS="${ARGUMENTS:-}"

# Parse only first two words — ignore trailing tokens.
# "|| true" guards against read returning 1 on empty input under set -e.
read -r SUBCMD REST _ <<< "$ARGUMENTS" || true
SUBCMD="${SUBCMD:-}"
REST="${REST:-}"

case "$SUBCMD" in
    activate)
        if set_all_hooks "yes"; then
            echo "All hooks activated."
        else
            echo ""
            echo "Some hooks could not be activated (see warnings above)."
        fi
        echo ""
        show_status
        ;;

    deactivate)
        if set_all_hooks "no"; then
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
        canonical=$(resolve_hook_name "$SUBCMD") || {
            echo "Unknown subcommand or hook type: $SUBCMD"
            echo ""
            show_help
            exit 1
        }

        case "$REST" in
            on)
                if set_hook "$canonical" "yes"; then
                    echo "$canonical = yes (enabled)"
                else
                    exit 1
                fi
                ;;
            off)
                if set_hook "$canonical" "no"; then
                    echo "$canonical = no (disabled)"
                else
                    exit 1
                fi
                ;;
            "")
                echo "Missing 'on' or 'off' after hook type."
                echo ""
                echo "Usage: /monitor-hooks $canonical on"
                echo "       /monitor-hooks $canonical off"
                exit 1
                ;;
            *)
                echo "Expected 'on' or 'off' after hook type, got: '$REST'"
                echo ""
                echo "Usage: /monitor-hooks $canonical on"
                echo "       /monitor-hooks $canonical off"
                exit 1
                ;;
        esac
        ;;
esac
```
