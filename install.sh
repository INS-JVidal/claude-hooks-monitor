#!/bin/bash
#
# Claude Code Hooks Monitor — Installer (downloads precompiled binaries)
# Usage: curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
#
# Downloads precompiled binaries from GitHub Releases by default.
# Falls back to building from source if download fails.
#
# Requires: Git, curl or wget.
# Go (>= 1.24) is only needed for source builds.
# On macOS with Homebrew, missing dependencies are installed automatically.
#
# Environment variables:
#   INSTALL_DIR        — where to clone the repo (default: ~/claude-hooks-monitor)
#   VERSION            — pin a specific release (e.g. v0.4.3; default: latest)
#   BUILD_FROM_SOURCE  — set to 1 to skip binary download and build from source
#

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
REPO_URL="https://github.com/INS-JVidal/claude-hooks-monitor.git"
GITHUB_REPO="INS-JVidal/claude-hooks-monitor"
INSTALL_DIR="${INSTALL_DIR:-$HOME/claude-hooks-monitor}"
MIN_GO_VERSION="1.24"
SETUP_SH_URL="https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/setup.sh"
BINARIES_DOWNLOADED=false
TEMP_DIR=""

# ── Colors ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
CHECK="${GREEN}✓${NC}"
CROSS="${RED}✗${NC}"
ARROW="${BLUE}→${NC}"

# ── Helpers ──────────────────────────────────────────────────────────────────

info()  { echo -e "${ARROW} $*"; }
ok()    { echo -e "${CHECK} $*"; }
warn()  { echo -e "${YELLOW}⚠${NC} $*"; }
fail()  { echo -e "${CROSS} $*"; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

# Compare version1 >= version2
version_ge() {
    local sorted_first
    sorted_first=$(printf '%s\n%s' "$1" "$2" | sort -V | head -n1)
    [ "$sorted_first" = "$2" ]
}

# Extract a version number like 1.21.6 from arbitrary output
extract_version() {
    echo "$1" | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n 1
}

# Clean up temp dir on exit (covers ctrl-c, errors, normal exit)
cleanup_temp() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}
trap cleanup_temp EXIT

# ── Argument parsing ────────────────────────────────────────────────────────

parse_args() {
    for arg in "$@"; do
        case "$arg" in
            --help|-h)
                echo "Usage: install.sh"
                echo ""
                echo "Downloads precompiled binaries from GitHub Releases."
                echo "Falls back to building from source if download fails."
                echo "Requires Git and curl or wget."
                echo ""
                echo "Environment variables:"
                echo "  INSTALL_DIR=<path>    Install location (default: ~/claude-hooks-monitor)"
                echo "  VERSION=<tag>         Pin a release version (e.g. v0.4.3; default: latest)"
                echo "  BUILD_FROM_SOURCE=1   Skip binary download, build from source (requires Go >= $MIN_GO_VERSION)"
                exit 0
                ;;
            *)
                warn "Unknown argument: $arg"
                ;;
        esac
    done
}

# ── Platform detection ───────────────────────────────────────────────────────

detect_platform() {
    local os
    os="$(uname -s)"
    case "$os" in
        Linux)
            GOOS="linux"
            if [ -f /etc/os-release ]; then
                # shellcheck disable=SC1091
                . /etc/os-release
                case "${ID:-}${ID_LIKE:-}" in
                    *ubuntu*|*debian*) PLATFORM="debian" ;;
                    *fedora*|*rhel*)   PLATFORM="fedora" ;;
                    *arch*)            PLATFORM="arch"   ;;
                    *)                 PLATFORM="linux"  ;;
                esac
            else
                PLATFORM="linux"
            fi
            ;;
        Darwin)
            GOOS="darwin"
            PLATFORM="macos"
            ;;
        *)
            GOOS="unknown"
            PLATFORM="unknown"
            ;;
    esac
}

detect_arch() {
    local machine
    machine="$(uname -m)"
    case "$machine" in
        x86_64)         GOARCH="amd64" ;;
        aarch64|arm64)  GOARCH="arm64" ;;
        i386|i686)      GOARCH="386"   ;;
        *)              GOARCH="unknown" ;;
    esac
}

# ── Binary download ─────────────────────────────────────────────────────────

download_binaries() {
    # Skip if user explicitly wants source build
    if [ "${BUILD_FROM_SOURCE:-0}" = "1" ]; then
        info "BUILD_FROM_SOURCE=1 — skipping binary download"
        return 0
    fi

    # Need curl or wget
    if ! command_exists curl && ! command_exists wget; then
        warn "Neither curl nor wget available — falling back to source build"
        return 0
    fi

    # Can't download for unknown platform/arch
    if [ "$GOOS" = "unknown" ] || [ "$GOARCH" = "unknown" ]; then
        warn "Unknown platform ($GOOS/$GOARCH) — falling back to source build"
        return 0
    fi

    info "Attempting to download precompiled binaries..."

    # Resolve version
    local version=""
    if [ -n "${VERSION:-}" ]; then
        version="$VERSION"
    else
        # Query GitHub API for latest release tag
        local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
        local api_response=""
        if command_exists curl; then
            api_response=$(curl -sS --max-time 15 "$api_url" 2>/dev/null) || true
        else
            api_response=$(wget -qO- --timeout=15 "$api_url" 2>/dev/null) || true
        fi

        if [ -n "$api_response" ]; then
            # Extract tag_name from JSON (avoid jq dependency)
            version=$(echo "$api_response" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | grep -o '"v[^"]*"' | tr -d '"')
        fi

        if [ -z "$version" ]; then
            warn "Could not determine latest release version — falling back to source build"
            return 0
        fi
    fi

    # GoReleaser strips the v prefix from filenames
    local version_no_v="${version#v}"
    local archive_name="claude-hooks-monitor_${version_no_v}_${GOOS}_${GOARCH}.tar.gz"
    local base_url="https://github.com/${GITHUB_REPO}/releases/download/${version}"
    local archive_url="${base_url}/${archive_name}"
    local checksums_url="${base_url}/checksums.txt"

    info "Version: ${version} (${GOOS}/${GOARCH})"

    # Create temp dir
    TEMP_DIR=$(mktemp -d)

    # Download archive and checksums
    local dl_ok=true
    if command_exists curl; then
        curl -sSL --max-time 60 --fail -o "${TEMP_DIR}/${archive_name}" "$archive_url" 2>/dev/null || dl_ok=false
        if [ "$dl_ok" = true ]; then
            curl -sSL --max-time 30 --fail -o "${TEMP_DIR}/checksums.txt" "$checksums_url" 2>/dev/null || dl_ok=false
        fi
    else
        wget -q --timeout=60 -O "${TEMP_DIR}/${archive_name}" "$archive_url" 2>/dev/null || dl_ok=false
        if [ "$dl_ok" = true ]; then
            wget -q --timeout=30 -O "${TEMP_DIR}/checksums.txt" "$checksums_url" 2>/dev/null || dl_ok=false
        fi
    fi

    if [ "$dl_ok" != true ]; then
        warn "Download failed — falling back to source build"
        return 0
    fi

    ok "Downloaded ${archive_name}"

    # Verify checksum
    if command_exists sha256sum; then
        local expected_checksum
        expected_checksum=$(grep "$archive_name" "${TEMP_DIR}/checksums.txt" | awk '{print $1}')
        if [ -z "$expected_checksum" ]; then
            warn "Archive not found in checksums.txt — falling back to source build"
            return 0
        fi
        local actual_checksum
        actual_checksum=$(sha256sum "${TEMP_DIR}/${archive_name}" | awk '{print $1}')
        if [ "$expected_checksum" != "$actual_checksum" ]; then
            warn "Checksum mismatch — falling back to source build"
            warn "  expected: ${expected_checksum}"
            warn "  actual:   ${actual_checksum}"
            return 0
        fi
        ok "Checksum verified"
    elif command_exists shasum; then
        # macOS fallback
        local expected_checksum
        expected_checksum=$(grep "$archive_name" "${TEMP_DIR}/checksums.txt" | awk '{print $1}')
        if [ -z "$expected_checksum" ]; then
            warn "Archive not found in checksums.txt — falling back to source build"
            return 0
        fi
        local actual_checksum
        actual_checksum=$(shasum -a 256 "${TEMP_DIR}/${archive_name}" | awk '{print $1}')
        if [ "$expected_checksum" != "$actual_checksum" ]; then
            warn "Checksum mismatch — falling back to source build"
            warn "  expected: ${expected_checksum}"
            warn "  actual:   ${actual_checksum}"
            return 0
        fi
        ok "Checksum verified"
    else
        warn "sha256sum/shasum not found — skipping checksum verification"
    fi

    # Extract to temp dir
    tar -xzf "${TEMP_DIR}/${archive_name}" -C "${TEMP_DIR}" 2>/dev/null || {
        warn "Archive extraction failed — falling back to source build"
        return 0
    }

    # Verify both binaries exist in the extract
    if [ ! -f "${TEMP_DIR}/claude-hooks-monitor" ] || [ ! -f "${TEMP_DIR}/hook-client" ]; then
        warn "Expected binaries not found in archive — falling back to source build"
        return 0
    fi

    BINARIES_DOWNLOADED=true
    ok "Precompiled binaries ready"
}

# Install downloaded binaries into the repo directory
install_binaries() {
    mkdir -p "$INSTALL_DIR/bin"

    # claude-hooks-monitor → bin/monitor
    cp "${TEMP_DIR}/claude-hooks-monitor" "$INSTALL_DIR/bin/monitor"
    chmod +x "$INSTALL_DIR/bin/monitor"

    # hook-client → hooks/hook-client
    mkdir -p "$INSTALL_DIR/hooks"
    cp "${TEMP_DIR}/hook-client" "$INSTALL_DIR/hooks/hook-client"
    chmod +x "$INSTALL_DIR/hooks/hook-client"

    ok "Binaries installed to $INSTALL_DIR"
}

# ── Prerequisite checks ─────────────────────────────────────────────────────

check_prerequisites() {
    local missing=()

    # git
    if ! command_exists git; then
        missing+=("git")
    fi

    # make (optional — warn but don't fail)
    if ! command_exists make; then
        warn "make not found — you can still build, but 'make run' and other targets won't work."
    fi

    # go (with version check)
    if command_exists go; then
        local go_ver
        go_ver=$(extract_version "$(go version 2>&1)")
        if [ -n "$go_ver" ] && version_ge "$go_ver" "$MIN_GO_VERSION"; then
            ok "Go $go_ver"
        else
            missing+=("go (>= $MIN_GO_VERSION)")
        fi
    else
        missing+=("go (>= $MIN_GO_VERSION)")
    fi

    if [ ${#missing[@]} -eq 0 ]; then
        return 0
    fi

    # ── Missing deps — platform-specific guidance ────────────────────────
    echo ""
    warn "Missing prerequisites: ${missing[*]}"
    echo ""

    case "$PLATFORM" in
        debian)
            info "Detected Ubuntu/Debian — running setup.sh to install system dependencies..."
            echo ""
            install_debian_deps
            # Re-check after install
            if ! command_exists go || ! command_exists git; then
                fail "Some dependencies are still missing after setup.sh. Check the output above."
            fi
            ;;
        macos)
            if command_exists brew; then
                info "Installing missing dependencies via Homebrew..."
                local brew_pkgs=()
                for dep in "${missing[@]}"; do
                    case "$dep" in
                        go*) brew_pkgs+=("go") ;;
                        git) brew_pkgs+=("git") ;;
                    esac
                done
                if [ ${#brew_pkgs[@]} -gt 0 ]; then
                    brew install "${brew_pkgs[@]}"
                fi
                # Re-source PATH for newly installed Go
                if [ -d "$(brew --prefix)/opt/go/bin" ]; then
                    export PATH="$PATH:$(brew --prefix)/opt/go/bin"
                fi
                # Re-verify after install
                if ! command_exists go || ! command_exists git; then
                    fail "Some dependencies are still missing after Homebrew install. Check the output above."
                fi
                ok "Dependencies installed via Homebrew"
            else
                echo -e "${YELLOW}Install missing tools with Homebrew:${NC}"
                echo ""
                echo "  brew install go git make"
                echo ""
                echo "If you don't have Homebrew: https://brew.sh"
                echo ""
                fail "Install Homebrew first (https://brew.sh), then re-run this script."
            fi
            ;;
        fedora)
            echo -e "${YELLOW}Install missing tools:${NC}"
            echo ""
            echo "  sudo dnf install golang git make"
            echo ""
            fail "Install the missing dependencies and re-run this script."
            ;;
        arch)
            echo -e "${YELLOW}Install missing tools:${NC}"
            echo ""
            echo "  sudo pacman -S go git make"
            echo ""
            fail "Install the missing dependencies and re-run this script."
            ;;
        *)
            echo -e "${YELLOW}Please install the following manually:${NC}"
            echo ""
            echo "  - Go >= $MIN_GO_VERSION : https://go.dev/dl/"
            echo "  - git                   : https://git-scm.com/"
            echo "  - make (optional)       : usually in build-essential or base-devel"
            echo ""
            fail "Install the missing dependencies and re-run this script."
            ;;
    esac
}

# ── Debian/Ubuntu: fetch and run setup.sh ────────────────────────────────────

install_debian_deps() {
    local setup_script="/tmp/claude-hooks-setup-$$.sh"

    if command_exists curl; then
        curl -sSL --fail --max-time 60 "$SETUP_SH_URL" -o "$setup_script" || {
            rm -f "$setup_script"
            fail "Failed to download setup.sh — check your network connection."
        }
    elif command_exists wget; then
        wget -q --timeout=60 "$SETUP_SH_URL" -O "$setup_script" || {
            rm -f "$setup_script"
            fail "Failed to download setup.sh — check your network connection."
        }
    else
        fail "Neither curl nor wget available to download setup.sh"
    fi

    # Sanity check: script must be non-empty
    if [ ! -s "$setup_script" ]; then
        rm -f "$setup_script"
        fail "Downloaded setup.sh is empty — aborting."
    fi

    chmod +x "$setup_script"
    bash "$setup_script"
    local rc=$?
    rm -f "$setup_script"

    # Re-source PATH in case Go was just installed
    export PATH="$PATH:/usr/local/go/bin:$HOME/.cargo/bin"

    return $rc
}

# ── Clone or update ──────────────────────────────────────────────────────────

clone_or_update() {
    if [ -d "$INSTALL_DIR/.git" ]; then
        info "Repository already exists at $INSTALL_DIR — pulling latest changes..."
        git -C "$INSTALL_DIR" pull --ff-only || {
            warn "git pull failed — continuing with existing code"
        }
    elif [ -d "$INSTALL_DIR" ]; then
        warn "$INSTALL_DIR exists but is not a git repository."
        warn "Remove it or set INSTALL_DIR to a different path:"
        echo ""
        echo "  rm -rf $INSTALL_DIR"
        echo "  # or"
        echo "  INSTALL_DIR=~/my-hooks-monitor curl -sSL ... | bash"
        echo ""
        fail "Cannot clone into existing non-git directory."
    else
        info "Cloning repository to $INSTALL_DIR..."
        git clone "$REPO_URL" "$INSTALL_DIR" || {
            # Clean up partial clone so re-runs don't hit "non-git directory" error.
            rm -rf "$INSTALL_DIR"
            fail "Repository clone failed. Check your network connection and try again."
        }
    fi
    ok "Repository ready at $INSTALL_DIR"
}

# ── Build ────────────────────────────────────────────────────────────────────

build_project() {
    info "Building monitor and hook-client..."
    if command_exists make; then
        make -C "$INSTALL_DIR" build
    else
        info "make not found — building with go build directly..."
        mkdir -p "$INSTALL_DIR/bin"
        local build_version
        build_version=$(cd "$INSTALL_DIR" && git describe --tags --always --dirty 2>/dev/null || echo "dev")
        local build_ldflags="-s -w -X main.version=${build_version}"
        (cd "$INSTALL_DIR" && go build -ldflags="$build_ldflags" -o bin/monitor ./cmd/monitor)
        (cd "$INSTALL_DIR" && go build -ldflags="$build_ldflags" -o hooks/hook-client ./cmd/hook-client)
    fi
    ok "Build complete"
}

# ── Verify ───────────────────────────────────────────────────────────────────

verify_build() {
    local ok_count=0

    if [ -x "$INSTALL_DIR/bin/monitor" ]; then
        ok "bin/monitor exists"
        ok_count=$((ok_count + 1))
    else
        warn "bin/monitor not found"
    fi

    if [ -x "$INSTALL_DIR/hooks/hook-client" ]; then
        ok "hooks/hook-client exists"
        ok_count=$((ok_count + 1))
    else
        warn "hooks/hook-client not found"
    fi

    if [ "$ok_count" -lt 2 ]; then
        fail "Build verification failed — expected bin/monitor and hooks/hook-client"
    fi
}

# ── System-wide install ──────────────────────────────────────────────────────

check_path_includes_local_bin() {
    case ":$PATH:" in
        *":$HOME/.local/bin:"*)
            ok "~/.local/bin is on PATH"
            ;;
        *)
            warn "~/.local/bin is not on PATH"
            echo "  Add to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
            echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
            echo ""
            ;;
    esac
}

register_global_hooks() {
    mkdir -p "$HOME/.claude"
    local output
    output=$("$INSTALL_DIR/hooks/hook-client" install-hooks 2>&1) || {
        warn "Failed to register hooks in ~/.claude/settings.json"
        echo "  Run 'make install-hooks' from the repo to register hooks later."
        return 0
    }
    # Show the actual message from hook-client (registered vs already present)
    ok "$output"
}

install_slash_command() {
    if [ -f "$INSTALL_DIR/scripts/monitor-hooks-global.md" ]; then
        cp "$INSTALL_DIR/scripts/monitor-hooks-global.md" "$HOME/.claude/commands/monitor-hooks.md"
        ok "Installed /monitor-hooks command to ~/.claude/commands/"
    else
        warn "Global slash command template not found — skipping"
        echo "  Run 'make install-hooks' from the repo to install it later."
    fi
}

install_systemwide() {
    info "Installing system-wide..."
    echo ""

    # Binaries → ~/.local/bin/
    mkdir -p "$HOME/.local/bin"
    cp "$INSTALL_DIR/bin/monitor" "$HOME/.local/bin/claude-hooks-monitor"
    cp "$INSTALL_DIR/hooks/hook-client" "$HOME/.local/bin/hook-client"
    chmod +x "$HOME/.local/bin/claude-hooks-monitor" "$HOME/.local/bin/hook-client"
    ok "Binaries installed to ~/.local/bin/"

    # Config → ~/.config/claude-hooks-monitor/
    mkdir -p "$HOME/.config/claude-hooks-monitor"
    cp -n "$INSTALL_DIR/hooks/hook_monitor.conf" "$HOME/.config/claude-hooks-monitor/" 2>/dev/null || true
    ok "Config installed to ~/.config/claude-hooks-monitor/"

    # Global hooks → ~/.claude/settings.json
    mkdir -p "$HOME/.claude/commands"
    register_global_hooks

    # Slash command → ~/.claude/commands/monitor-hooks.md
    install_slash_command

    # PATH check
    echo ""
    check_path_includes_local_bin
}

# ── Next steps ───────────────────────────────────────────────────────────────

print_next_steps() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  Installation complete!${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "${ARROW} ${GREEN}Start the monitor:${NC}"
    echo ""
    echo "  claude-hooks-monitor            # if ~/.local/bin is on PATH"
    echo "  claude-hooks-monitor --ui       # interactive tree UI"
    echo ""
    echo -e "${ARROW} ${GREEN}Use with Claude Code:${NC}"
    echo ""
    echo "  Hooks are registered globally — start claude in any project"
    echo "  and events will appear in the monitor automatically."
    echo ""
    echo "  Use /monitor-hooks status inside claude to check the setup."
    echo ""
    echo -e "${ARROW} ${GREEN}Test the installation:${NC}"
    echo ""
    echo "  cd $INSTALL_DIR"
    echo "  make run           # terminal 1"
    echo "  make test          # terminal 2"
    echo ""
    echo -e "  For detailed instructions see: ${BLUE}$INSTALL_DIR/INSTALLME.md${NC}"
    echo ""
}

# ── Banner ───────────────────────────────────────────────────────────────────

print_banner() {
    echo ""
    echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║       Claude Code Hooks Monitor — Installer               ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

# ── Main ─────────────────────────────────────────────────────────────────────

main() {
    parse_args "$@"
    print_banner
    detect_platform
    detect_arch
    info "Platform: $PLATFORM ($GOOS/$GOARCH)"
    info "Install directory: $INSTALL_DIR"
    echo ""

    download_binaries
    echo ""
    clone_or_update
    echo ""

    if [ "$BINARIES_DOWNLOADED" = true ]; then
        install_binaries || {
            warn "Binary installation failed — falling back to source build"
            BINARIES_DOWNLOADED=false
            check_prerequisites
            echo ""
            build_project
        }
    else
        check_prerequisites
        echo ""
        build_project
    fi
    echo ""
    verify_build
    echo ""
    install_systemwide

    print_next_steps
}

main "$@"
