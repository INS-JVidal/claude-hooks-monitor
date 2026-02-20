#!/usr/bin/env -S uv run --quiet --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "requests",
# ]
# ///
"""
Claude Code Hooks Monitor — Universal hook script.

Reads hook event JSON from stdin, checks the toggle config, and forwards
enabled events to the Go monitor server via HTTP POST.

CRITICAL: This script must ALWAYS exit 0 and NEVER write to stdout.
  - Exit non-zero can block Claude Code operations (exit 2 = blocking error).
  - Stdout is reserved for hook decision JSON (allow/deny/modify).
"""

import configparser
import json
import os
import sys
from datetime import datetime, timezone
from typing import Any


# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

MONITOR_URL = os.getenv("HOOK_MONITOR_URL", "http://localhost:8080")
CONFIG_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "hook_monitor.conf")


def _safe_int(value: str | None, default: int) -> int:
    """Parse an integer from a string, returning *default* on any failure."""
    try:
        return int(value)  # type: ignore[arg-type]
    except (ValueError, TypeError):
        return default


TIMEOUT = _safe_int(os.getenv("HOOK_TIMEOUT", "5"), 5)


# ---------------------------------------------------------------------------
# Core functions
# ---------------------------------------------------------------------------

def is_hook_enabled(hook_name: str) -> bool:
    """Check hook_monitor.conf to see if *hook_name* is enabled.

    Design:
    - Fail-open: missing file, missing key, or any error → True (enabled).
    - Uses optionxform=str to preserve PascalCase key names.
    """
    try:
        parser = configparser.ConfigParser()
        parser.optionxform = str  # preserve case — critical for PascalCase hook names
        parser.read(CONFIG_PATH)  # returns [] on missing file, does not raise
        value = parser.get("hooks", hook_name, fallback="yes")
        return value.strip().lower() != "no"
    except Exception:
        return True


def read_stdin() -> dict[str, Any]:
    """Read and parse JSON from stdin.

    Returns the parsed dict, or an error dict on failure.
    """
    data = ""
    try:
        data = sys.stdin.read()
        return json.loads(data) if data.strip() else {}
    except (json.JSONDecodeError, Exception) as exc:
        return {"error": str(exc), "raw_input": data[:2000]}


def send_to_monitor(hook_type: str, data: dict[str, Any]) -> bool:
    """POST event data to the monitor server.

    Returns True on success. Fails silently on connection errors (server may
    not be running) and on timeouts.
    """
    import requests  # lazy import — only when actually sending

    url = f"{MONITOR_URL}/hook/{hook_type}"
    try:
        resp = requests.post(
            url,
            json=data,
            headers={"Content-Type": "application/json"},
            timeout=TIMEOUT,
        )
        return resp.status_code == 200
    except requests.ConnectionError:
        # Server not running — expected during normal use without monitor.
        return False
    except Exception as exc:
        print(f"[hook_monitor] warning: {exc}", file=sys.stderr)
        return False


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    hook_data = read_stdin()
    hook_type = hook_data.get("hook_event_name", "Unknown")

    if not is_hook_enabled(hook_type):
        return  # skip — config says this hook is disabled

    hook_data["_monitor"] = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "project_dir": hook_data.get("cwd", ""),
        "plugin_root": os.getenv("CLAUDE_PLUGIN_ROOT", ""),
        "is_remote": os.getenv("CLAUDE_CODE_REMOTE", "") == "true",
    }

    send_to_monitor(hook_type, hook_data)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass  # never crash, never block Claude
    # Always exit 0, never write to stdout.
    sys.exit(0)
