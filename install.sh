#!/bin/bash
#
# Claude Code Hooks Monitor — Installer
# Usage: curl -sSL https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/install.sh | bash
#
# Environment variables:
#   INSTALL_DIR  — where to clone the repo (default: ~/claude-hooks-monitor)
#

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
REPO_URL="https://github.com/INS-JVidal/claude-hooks-monitor.git"
INSTALL_DIR="${INSTALL_DIR:-$HOME/claude-hooks-monitor}"
MIN_GO_VERSION="1.21"
SETUP_SH_URL="https://raw.githubusercontent.com/INS-JVidal/claude-hooks-monitor/main/setup.sh"

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

# ── Platform detection ───────────────────────────────────────────────────────

detect_platform() {
    local os
    os="$(uname -s)"
    case "$os" in
        Linux)
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
            PLATFORM="macos"
            ;;
        *)
            PLATFORM="unknown"
            ;;
    esac
}

# ── Prerequisite checks ─────────────────────────────────────────────────────

check_prerequisites() {
    local missing=()

    # git
    if ! command_exists git; then
        missing+=("git")
    fi

    # make
    if ! command_exists make; then
        missing+=("make")
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
            if ! command_exists go || ! command_exists git || ! command_exists make; then
                fail "Some dependencies are still missing after setup.sh. Check the output above."
            fi
            ;;
        macos)
            echo -e "${YELLOW}Install missing tools with Homebrew:${NC}"
            echo ""
            echo "  brew install go git make"
            echo ""
            echo "If you don't have Homebrew: https://brew.sh"
            echo ""
            fail "Install the missing dependencies and re-run this script."
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
            echo "  - make                  : (usually in build-essential or base-devel)"
            echo ""
            fail "Install the missing dependencies and re-run this script."
            ;;
    esac
}

# ── Debian/Ubuntu: fetch and run setup.sh ────────────────────────────────────

install_debian_deps() {
    local setup_script="/tmp/claude-hooks-setup-$$.sh"

    if command_exists curl; then
        curl -sSL "$SETUP_SH_URL" -o "$setup_script"
    elif command_exists wget; then
        wget -q "$SETUP_SH_URL" -O "$setup_script"
    else
        fail "Neither curl nor wget available to download setup.sh"
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
        git clone "$REPO_URL" "$INSTALL_DIR"
    fi
    ok "Repository ready at $INSTALL_DIR"
}

# ── Build ────────────────────────────────────────────────────────────────────

build_project() {
    info "Building monitor and hook-client..."
    make -C "$INSTALL_DIR" build
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

# ── Next steps ───────────────────────────────────────────────────────────────

print_next_steps() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  Installation complete!${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "${ARROW} ${GREEN}Start the monitor:${NC}"
    echo ""
    echo "  cd $INSTALL_DIR"
    echo "  make run          # console mode"
    echo "  make run-ui       # interactive tree UI"
    echo ""
    echo -e "${ARROW} ${GREEN}Configure hooks in your own project:${NC}"
    echo ""
    echo "  cd $INSTALL_DIR"
    echo "  make show-hooks-config"
    echo ""
    echo "  Copy the JSON output into your project's .claude/settings.json."
    echo "  Then start 'claude' in your project — events will appear in the monitor."
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
    print_banner
    detect_platform
    info "Platform: $PLATFORM"
    info "Install directory: $INSTALL_DIR"
    echo ""

    check_prerequisites
    echo ""

    clone_or_update
    echo ""

    build_project
    echo ""

    verify_build
    print_next_steps
}

main "$@"
