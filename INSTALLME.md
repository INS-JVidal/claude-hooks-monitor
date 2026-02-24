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

**Optional (for test suite):**

| Tool | Purpose | Install Link |
|------|---------|-------------|
| **jq** | JSON processing (tests) | [jqlang.github.io/jq](https://jqlang.github.io/jq/) |

> **Note:** Python is **not** required. Hook registration in `~/.claude/settings.json` is handled natively by the Go `hook-client install-hooks` subcommand.

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
6. **Installs system-wide** — copies binaries to `~/.local/bin/`, config to `~/.config/claude-hooks-monitor/`, registers global hooks in `~/.claude/settings.json`, and installs the `/monitor-hooks` slash command
7. Checks that `~/.local/bin` is on your PATH

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

**Option A — Use the setup script** (installs Go, jq, git, make):

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
go build -ldflags="-s -w" -o bin\monitor.exe .\cmd\monitor
go build -ldflags="-s -w" -o hooks\hook-client.exe .\cmd\hook-client
```

Or with Make (if installed):

```powershell
make build
```

---

## Configuring Claude Code Hooks

### Automatic setup (recommended)

The `install.sh` script and `make install && make install-hooks` both perform a complete system-wide setup:

- Binaries → `~/.local/bin/claude-hooks-monitor` and `~/.local/bin/hook-client`
- Config → `~/.config/claude-hooks-monitor/hook_monitor.conf`
- Global hooks → `~/.claude/settings.json` (uses bare `hook-client` command, found via PATH)
- Slash command → `~/.claude/commands/monitor-hooks.md`

After installation, hooks fire automatically in **every** Claude Code session — no per-project configuration needed.

```bash
# Terminal 1: start the monitor
claude-hooks-monitor

# Terminal 2: work in any project
cd ~/my-project
claude    # hooks fire automatically
```

> **Important:** `~/.local/bin` must be on your PATH for the global hooks to find `hook-client`. The installer checks this and prints instructions if it's missing.

### Scenario A: Running `claude` inside the monitor project

This works out of the box even without system-wide install. The repository includes a `.claude/settings.json` that uses `$CLAUDE_PROJECT_DIR` to find the hook-client relative to the project:

```bash
cd claude-hooks-monitor
make run        # terminal 1
claude          # terminal 2 — hooks fire automatically
```

No extra configuration needed.

### Scenario B: Manual per-project setup (advanced)

If you prefer not to install system-wide, you can configure hooks for a specific project using absolute paths:

**Step 1:** Generate the config snippet with the correct path:

```bash
cd ~/claude-hooks-monitor   # or wherever you installed it
make show-hooks-config
```

**Step 2:** Copy the output into your project's `.claude/settings.json`.

**Step 3:** Start the monitor and Claude in separate terminals.

### How `$CLAUDE_PROJECT_DIR` works

When Claude Code runs, it sets the `CLAUDE_PROJECT_DIR` environment variable to the root of the project it's working in (where `.claude/settings.json` lives). The in-repo settings.json uses this:

```json
"command": "\"$CLAUDE_PROJECT_DIR\"/hooks/hook-client"
```

This only works when Claude is running *inside* the monitor project. The system-wide install avoids this limitation by placing `hook-client` on PATH.

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
2. **Port mismatch:** The hook-client reads the port from `.monitor-port` in `~/.config/claude-hooks-monitor/` (system-wide install) or `hooks/.monitor-port` (local). If you're running the server on a custom port, set `HOOK_MONITOR_URL=http://localhost:<port>`.
3. **Test manually:** `make send-test-hook` — if this shows an event, the server works and the issue is in hook delivery.

### "permission denied" on hook-client

```bash
chmod +x hooks/hook-client
# Or rebuild:
make build-hook-client
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
# Remove system-wide components
rm -f ~/.local/bin/claude-hooks-monitor ~/.local/bin/hook-client
rm -rf ~/.config/claude-hooks-monitor
rm -f ~/.claude/commands/monitor-hooks.md

# Remove hooks from global settings
# Edit ~/.claude/settings.json and delete the "hooks" block

# Remove the project
rm -rf ~/claude-hooks-monitor   # or wherever you installed it
```

If you used `setup.sh` to install system dependencies (Go, etc.), those remain installed system-wide. Remove them individually if desired:

```bash
# Remove Go
sudo rm -rf /usr/local/go
# Remove the PATH export from ~/.bashrc
```
