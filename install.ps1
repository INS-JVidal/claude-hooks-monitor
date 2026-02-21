#
# Claude Code Hooks Monitor — Windows Installer (PowerShell)
# Usage: .\install.ps1
#
# Requires: Go (>= 1.21), Git. Make is optional.
# If missing, suggests installation via winget.
#
# Environment variables:
#   INSTALL_DIR  — where to clone the repo (default: $HOME\claude-hooks-monitor)
#

param(
    [string]$InstallDir = ""
)

$ErrorActionPreference = "Stop"

# ── Configuration ────────────────────────────────────────────────────────────
$RepoUrl = "https://github.com/INS-JVidal/claude-hooks-monitor.git"
$MinGoVersion = "1.21"

if ($InstallDir -eq "") {
    $InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $HOME "claude-hooks-monitor" }
}

# ── Helpers ──────────────────────────────────────────────────────────────────

function Write-Info  { param([string]$msg) Write-Host "  -> $msg" -ForegroundColor Blue }
function Write-Ok    { param([string]$msg) Write-Host "  [OK] $msg" -ForegroundColor Green }
function Write-Warn  { param([string]$msg) Write-Host "  [!] $msg" -ForegroundColor Yellow }
function Write-Fail  { param([string]$msg) Write-Host "  [X] $msg" -ForegroundColor Red; exit 1 }

function Test-Command {
    param([string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-SemVer {
    param([string]$Text)
    if ($Text -match '(\d+\.\d+(\.\d+)?)') {
        return $Matches[1]
    }
    return $null
}

function Test-VersionGe {
    param([string]$Have, [string]$Need)
    try {
        return ([version]$Have -ge [version]$Need)
    } catch {
        return $false
    }
}

# ── Banner ───────────────────────────────────────────────────────────────────

function Show-Banner {
    Write-Host ""
    Write-Host "  ╔════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "  ║       Claude Code Hooks Monitor — Windows Installer       ║" -ForegroundColor Cyan
    Write-Host "  ╚════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
    Write-Host ""
}

# ── Prerequisite checks ─────────────────────────────────────────────────────

function Test-Prerequisites {
    $missing = @()

    # Git
    if (-not (Test-Command "git")) {
        $missing += "git"
    }

    # Go (with version check)
    if (Test-Command "go") {
        $goOutput = & go version 2>&1
        $goVer = Get-SemVer $goOutput
        if ($goVer -and (Test-VersionGe $goVer $MinGoVersion)) {
            Write-Ok "Go $goVer"
        } else {
            $missing += "go (>= $MinGoVersion)"
        }
    } else {
        $missing += "go (>= $MinGoVersion)"
    }

    # Make (optional)
    if (-not (Test-Command "make")) {
        Write-Warn "make not found — you can still build, but 'make run' and other targets won't work."
    }

    if ($missing.Count -eq 0) {
        return
    }

    Write-Host ""
    Write-Warn "Missing prerequisites: $($missing -join ', ')"
    Write-Host ""

    $hasWinget = Test-Command "winget"
    if ($hasWinget) {
        Write-Host "  Install with winget:" -ForegroundColor Yellow
        Write-Host ""
        foreach ($dep in $missing) {
            switch -Wildcard ($dep) {
                "go*"   { Write-Host "    winget install GoLang.Go" }
                "git"   { Write-Host "    winget install Git.Git" }
            }
        }
        Write-Host ""
    } else {
        Write-Host "  Install manually:" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "    Go  : https://go.dev/dl/"
        Write-Host "    Git : https://git-scm.com/download/win"
        Write-Host ""
    }

    Write-Fail "Install the missing dependencies and re-run this script."
}

# ── Clone or update ──────────────────────────────────────────────────────────

function Install-Repo {
    if (Test-Path (Join-Path $InstallDir ".git")) {
        Write-Info "Repository already exists at $InstallDir — pulling latest changes..."
        & git -C $InstallDir pull --ff-only
        if ($LASTEXITCODE -ne 0) {
            Write-Warn "git pull failed — continuing with existing code"
        }
    } elseif (Test-Path $InstallDir) {
        Write-Warn "$InstallDir exists but is not a git repository."
        Write-Host ""
        Write-Host "  Remove it or set INSTALL_DIR to a different path:"
        Write-Host "    Remove-Item -Recurse -Force $InstallDir"
        Write-Host ""
        Write-Fail "Cannot clone into existing non-git directory."
    } else {
        Write-Info "Cloning repository to $InstallDir..."
        & git clone $RepoUrl $InstallDir
        if ($LASTEXITCODE -ne 0) {
            Write-Fail "git clone failed."
        }
    }
    Write-Ok "Repository ready at $InstallDir"
}

# ── Build ────────────────────────────────────────────────────────────────────

function Build-Project {
    Write-Info "Building monitor and hook-client..."

    $binDir = Join-Path $InstallDir "bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    }

    Push-Location $InstallDir
    try {
        & go build -ldflags="-s -w" -o "bin\monitor.exe" .
        if ($LASTEXITCODE -ne 0) { Write-Fail "Failed to build monitor." }

        & go build -ldflags="-s -w" -o "hooks\hook-client.exe" .\cmd\hook-client
        if ($LASTEXITCODE -ne 0) { Write-Fail "Failed to build hook-client." }
    } finally {
        Pop-Location
    }

    Write-Ok "Build complete"
}

# ── Verify ───────────────────────────────────────────────────────────────────

function Test-Build {
    $okCount = 0

    $monitorPath = Join-Path $InstallDir "bin\monitor.exe"
    if (Test-Path $monitorPath) {
        Write-Ok "bin\monitor.exe exists"
        $okCount++
    } else {
        Write-Warn "bin\monitor.exe not found"
    }

    $hookClientPath = Join-Path $InstallDir "hooks\hook-client.exe"
    if (Test-Path $hookClientPath) {
        Write-Ok "hooks\hook-client.exe exists"
        $okCount++
    } else {
        Write-Warn "hooks\hook-client.exe not found"
    }

    if ($okCount -lt 2) {
        Write-Fail "Build verification failed — expected bin\monitor.exe and hooks\hook-client.exe"
    }
}

# ── Next steps ───────────────────────────────────────────────────────────────

function Show-NextSteps {
    Write-Host ""
    Write-Host "  ═══════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host "    Installation complete!" -ForegroundColor Cyan
    Write-Host "  ═══════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Start the monitor:" -ForegroundColor Green
    Write-Host ""
    Write-Host "    cd $InstallDir"
    Write-Host "    .\bin\monitor.exe          # console mode"
    Write-Host "    .\bin\monitor.exe --ui     # interactive tree UI"
    Write-Host ""
    Write-Host "  Configure hooks in your own project:" -ForegroundColor Green
    Write-Host ""
    if (Test-Command "make") {
        Write-Host "    cd $InstallDir"
        Write-Host "    make show-hooks-config"
    } else {
        Write-Host "    See INSTALLME.md for hooks configuration instructions."
    }
    Write-Host ""
    Write-Host "  Copy the hooks JSON into your project's .claude\settings.json."
    Write-Host "  Then start 'claude' in your project — events will appear in the monitor."
    Write-Host ""
    Write-Host "  Note: On Windows, use backslash paths and .exe extension in settings.json:" -ForegroundColor Yellow
    Write-Host "    ""command"": ""$InstallDir\hooks\hook-client.exe""" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  For detailed instructions see: $InstallDir\INSTALLME.md" -ForegroundColor Blue
    Write-Host ""
}

# ── Main ─────────────────────────────────────────────────────────────────────

Show-Banner
Write-Info "Platform: Windows"
Write-Info "Install directory: $InstallDir"
Write-Host ""

Test-Prerequisites
Write-Host ""
Install-Repo
Write-Host ""
Build-Project
Write-Host ""
Test-Build

Show-NextSteps
