# Installation Guide — Claude Code Hooks Monitor

Detailed installation instructions for all platforms. For a quick start, see the [README](README.md).

## Requirements

| Tool | Minimum Version | Purpose | Install Link |
|------|----------------|---------|-------------|
| **Git** | any | Clone the repository | [git-scm.com](https://git-scm.com/) |
| **curl** or **wget** | any | Download precompiled binaries | (usually pre-installed) |
| **Make** | any (optional) | Build automation (`make run`, etc.) | (included in build-essential) |
| **Go** | 1.24+ (optional) | Only needed for source builds | [go.dev/dl](https://go.dev/dl/) |

> **Note:** The installer downloads precompiled binaries by default. Go is only required if you build from source (`BUILD_FROM_SOURCE=1`) or if the binary download fails.

**Optional (for test suite and alternative hook script):**

| Tool | Purpose | Install Link |
|------|---------|-------------|
| **Python** | 3.11+ — Alternative hook script | [python.org](https://www.python.org/downloads/) |
| **uv** | Python dependency management | [docs.astral.sh/uv](https://docs.astral.sh/uv/) |
| **jq** | JSON processing (tests) | [jqlang.github.io/jq](https://jqlang.github.io/jq/) |

---

## Quick Install

### Linux / macOS

One command to download precompiled binaries and set up:

```bash
curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

The installer:
1. Detects your platform and architecture (Linux/macOS, amd64/arm64)
2. Downloads precompiled binaries from GitHub Releases (with checksum verification)
3. Clones the repository (needed for config files, Makefile, .claude/)
4. Places binaries in the correct locations
5. Verifies both binaries exist
6. Prints next steps

If the binary download fails (e.g., no internet, unsupported platform), the installer falls back to building from source automatically.

To force a source build: `BUILD_FROM_SOURCE=1 curl -sSL ... | bash`
To pin a version: `VERSION=v0.4.3 curl -sSL ... | bash`

### Windows

PowerShell installer:

```powershell
Invoke-WebRequest -Uri "https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.ps1" -OutFile install.ps1
.\install.ps1
```

The installer checks for Go and Git, suggests `winget install` commands if missing, then clones and builds.

### Custom install location

```bash
INSTALL_DIR=~/projects/hooks-monitor \
  curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
```

On Windows:

```powershell
.\install.ps1 -InstallDir "C:\Users\you\projects\hooks-monitor"
```

Safe to run multiple times — if the repo already exists, it does `git pull` and rebuilds.

---

## Manual Installation

### Ubuntu / Debian

**Option A — Use the setup script** (installs Go, Python, uv, jq, git, make):

```bash
curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/setup.sh | bash
```

**Option B — Install manually:**

```bash
# Install build tools
sudo apt-get update
sudo apt-get install -y git make curl jq

# Install Go (latest stable)
GO_VERSION=$(curl -sL https://go.dev/VERSION?m=text | head -n1 | sed 's/go//')
curl -LO "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
export PATH=$PATH:/usr/local/go/bin
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
rm "go${GO_VERSION}.linux-amd64.tar.gz"
```

Then clone and build:

```bash
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
make build
```

### macOS

```bash
# Install with Homebrew
brew install go git make jq

# Clone and build
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
make build
```

### Fedora / RHEL

```bash
sudo dnf install golang git make jq curl
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
make build
```

### Arch Linux

```bash
sudo pacman -S go git make jq curl
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
make build
```

### Windows

**Option A — Use the PowerShell installer** (recommended):

```powershell
Invoke-WebRequest -Uri "https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.ps1" -OutFile install.ps1
.\install.ps1
```

**Option B — Manual:**

1. Install Go from [go.dev/dl](https://go.dev/dl/) or via `winget install GoLang.Go`
2. Install Git from [git-scm.com](https://git-scm.com/download/win) or via `winget install Git.Git`
3. Clone and build:

```powershell
git clone https://github.com/INS-JVidal/claude-hooks-monitor.git
cd claude-hooks-monitor
go build -ldflags="-s -w" -o bin\monitor.exe .
go build -ldflags="-s -w" -o hooks\hook-client.exe .\cmd\hook-client
```

Or with Make (if installed):

```powershell
make build
```

---

## Configuring Claude Code Hooks

After building, you need to tell Claude Code to send hook events to the monitor. There are two scenarios:

### Scenario A: Running `claude` inside the monitor project

This works out of the box. The repository includes a `.claude/settings.json` that uses `$CLAUDE_PROJECT_DIR` to find the hook-client relative to the project:

```bash
cd claude-hooks-monitor
make run        # terminal 1
claude          # terminal 2 — hooks fire automatically
```

No extra configuration needed.

### Scenario B: Monitoring hooks in your own project

If you want to monitor Claude Code hooks while working on a *different* project, you need to copy the hooks configuration into that project's `.claude/settings.json` with the **absolute path** to the hook-client binary.

**Step 1:** Generate the config snippet with the correct path:

```bash
cd ~/claude-hooks-monitor   # or wherever you installed it
make show-hooks-config
```

This prints a JSON `"hooks"` block with the absolute path to `hooks/hook-client` already filled in.

**Step 2:** Copy the output into your project's `.claude/settings.json`:

```bash
cd ~/my-project
mkdir -p .claude
# Create or edit .claude/settings.json and paste the hooks block
```

**Step 3:** Start the monitor and Claude:

```bash
# Terminal 1: start the monitor
cd ~/claude-hooks-monitor
make run

# Terminal 2: work in your project
cd ~/my-project
claude
```

### Full `.claude/settings.json` example

Below is a complete example. Replace `/home/you/claude-hooks-monitor` with your actual install path:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "SessionEnd": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "PreToolUse": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "PostToolUse": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "PostToolUseFailure": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "Notification": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "PermissionRequest": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "SubagentStart": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "SubagentStop": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "TeammateIdle": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "TaskCompleted": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "ConfigChange": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ],
    "PreCompact": [
      { "hooks": [{ "type": "command", "command": "\"/home/you/claude-hooks-monitor/hooks/hook-client\"" }] }
    ]
  }
}
```

### How `$CLAUDE_PROJECT_DIR` works

When Claude Code runs, it sets the `CLAUDE_PROJECT_DIR` environment variable to the root of the project it's working in (where `.claude/settings.json` lives). The in-repo settings.json uses this:

```json
"command": "\"$CLAUDE_PROJECT_DIR\"/hooks/hook-client"
```

This only works when Claude is running *inside* the monitor project. For external projects, use the absolute path as shown above.

---

## Verify Installation

```bash
cd claude-hooks-monitor

# Check the build produced both binaries
ls -la bin/monitor hooks/hook-client

# Check the server starts
make run &
sleep 2
make check         # should say "Server is running on port 8080"
make stats         # should return JSON with hook counts

# Run the full test suite
make test

# Stop the background server
kill %1
```

Expected `make test` output: 3 test phases pass (direct server, end-to-end, config toggle).

---

## Troubleshooting

### "go: command not found"

Go is not on your PATH. Common fixes:

```bash
# If installed via tarball to /usr/local/go
export PATH=$PATH:/usr/local/go/bin
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# If installed via Homebrew (macOS)
export PATH=$PATH:$(brew --prefix go)/bin

# Check it works
go version
```

### "make build fails"

1. **Go version too old:** Check `go version` — need >= 1.24. Reinstall if needed.
2. **Module issues:** Run `go mod tidy` to resolve dependencies, then `make build`.
3. **Network issues:** Go needs to download modules on first build. Check your internet connection.

### "hooks don't fire"

1. **Check Claude sees them:** Run `/hooks` inside a Claude Code session. You should see all hook types listed.
2. **Check settings.json path:** The `.claude/settings.json` must be at the root of the project where you're running `claude`.
3. **Check hook-client is executable:** `ls -la hooks/hook-client` — should show `rwxr-xr-x`. If not: `chmod +x hooks/hook-client`.
4. **Rebuild hook-client:** `make build-hook-client`.

### "server starts but no events appear"

1. **Is the server running?** `make check` — should say "Server is running on port 8080".
2. **Port mismatch:** The hook-client reads the port from `.hook-monitor-port` file in the project root. If you're running the server on a custom port, make sure `HOOK_MONITOR_URL` matches.
3. **Test manually:** `make send-test-hook` — if this shows an event, the server works and the issue is in hook delivery.

### "permission denied" on hook-client

```bash
chmod +x hooks/hook-client
# Or rebuild:
make build-hook-client
```

### "uv: command not found"

uv is only needed for the Python hook script alternative. If you're using the Go hook-client (default), you can ignore this.

To install uv:
```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
source ~/.bashrc
```

### Windows-specific issues

- **`.exe` extension:** All binaries need the `.exe` extension on Windows. If `make build` doesn't add it, build manually: `go build -o bin\monitor.exe .`
- **hook-client path in settings.json:** Use backslash paths and `.exe` extension:
  ```json
  "command": "C:\\Users\\you\\claude-hooks-monitor\\hooks\\hook-client.exe"
  ```
- **`make run-background`:** Not supported on Windows (uses `nohup`/`lsof`). Use `make run` in a separate terminal, or run `.\bin\monitor.exe` directly.
- **Claude Code + Windows hooks:** Claude Code's hook system on Windows may require the `.exe` extension and backslash paths. Test with `make show-hooks-config` and adjust as needed.

### WSL-specific issues

- **localhost resolution:** If `curl http://localhost:8080/health` fails from within WSL, try `curl http://127.0.0.1:8080/health`.
- **/mnt/c/ permissions:** Running the project from `/mnt/c/...` (Windows filesystem) causes slow I/O and permission issues. Clone the repo to your WSL home directory (`~/claude-hooks-monitor`) instead.
- **File watchers:** WSL2 has limited inotify support on `/mnt/c/`. This doesn't affect the monitor but may affect other tools.

### macOS-specific issues

- **Gatekeeper quarantine:** If macOS blocks the binary with "cannot be opened because the developer cannot be verified":
  ```bash
  xattr -d com.apple.quarantine bin/monitor
  xattr -d com.apple.quarantine hooks/hook-client
  ```
- **Port already in use:** `lsof -i:8080` to see what's using the port. Try `PORT=9000 make run`.

---

## Uninstalling

```bash
# Remove the project
rm -rf ~/claude-hooks-monitor   # or wherever you installed it

# Remove hooks from your project's .claude/settings.json
# Edit the file and delete the "hooks" block, or delete the whole file
rm .claude/settings.json
```

If you used `setup.sh` to install system dependencies (Go, Python, etc.), those remain installed system-wide. Remove them individually if desired:

```bash
# Remove Go
sudo rm -rf /usr/local/go
# Remove the PATH export from ~/.bashrc

# Remove uv
rm -rf ~/.cargo/bin/uv ~/.cargo/bin/uvx
```
