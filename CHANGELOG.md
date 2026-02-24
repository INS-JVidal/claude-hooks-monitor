# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.4.6] - 2026-02-22

### Fixed

- Critical: no-make fallback built monitor from `.` instead of `./cmd/monitor`
- Installer now falls back to source build if binary installation fails
- `register_global_hooks` now shows accurate message (registered vs already present)
- Makefile `install-hooks` target now propagates errors instead of silently swallowing them
- Local slash command missing project-level config override (synced with global version)
- Local slash command referenced wrong project name (`hooks4claude` → `claude-hooks-monitor`)
- Stale version number in EXAMPLES.md banner

## [0.4.5] - 2026-02-22

### Added

- Hook configuration menu in TUI (`H` key) — toggle individual hooks on/off
- `internal/config` package for INI-based hook config read/write
- Show build version in TUI header
- `hook-client install-hooks` subcommand — registers all 15 hooks in `~/.claude/settings.json` natively in Go

### Changed

- Remove Python dependency from installer and Makefile — hook registration now uses Go `hook-client install-hooks` with atomic file writes

### Fixed

- Truncate long input fields in detail pane after 10 lines

## [0.4.4] - 2026-02-22

### Added

- Precompiled binary download in installer, with fallback to source build
- Screenshots for Tree UI and `/monitor-hooks` activation in README

### Changed

- Rewrite README for Linux Ubuntu with three-part architecture overview
- Remove Python hook forwarder, use Go hook-client in test suite

### Fixed

- PreToolUse left disabled in hook config from interrupted test run
- 5 audit findings: config parity, JSON depth, detail caps, dropped counter

## [0.4.3] - 2026-02-21

### Fixed

- Critical TOCTOU race and 3 robustness findings from R3 analysis

## [0.4.2] - 2026-02-21

### Fixed

- Comprehensive robustness hardening from automated analysis

## [0.4.1] - 2026-02-21

### Added

- `/monitor-hooks` slash command with 17 robustness fixes
- TUI detail pane for inspecting event JSON
- Cross-platform support for Linux, macOS, and Windows
- `.goreleaser.yml` for multi-binary build

### Changed

- Reorganize to standard Go layout (`cmd/`, `internal/`)
- Show all JSON fields in TUI detail pane instead of hardcoded subset
- Rewrite ARCHITECTURE.md with comprehensive ASCII UML diagrams
- Harden server security: loopback binding, auth, timeouts, input validation

## [0.3.0] - 2026-02-21

### Added

- Installation flow and documentation for third-party users

### Changed

- Update README for v0.2.0: document TUI mode and refactored architecture

## [0.2.0] - 2026-02-21

### Added

- Interactive TUI mode with bubbletea tree view

### Fixed

- Review findings: robustness, shutdown, and rune safety

## [0.1.0] - 2026-02-20

### Added

- Go hook client with auto port discovery and single-instance guard
- HTTP monitor server with per-hook-type endpoints
- `/stats`, `/events`, `/health` endpoints

### Fixed

- Efficiency issues in monitor server and hook client
- Reduce default hook-client timeout from 5s to 2s

[Unreleased]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.4.6...HEAD
[0.4.6]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.4.5...v0.4.6
[0.4.5]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.4.4...v0.4.5
[0.4.4]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.4.3...v0.4.4
[0.4.3]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.3.0...v0.4.1
[0.3.0]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/INS-JVidal/claude-hooks-monitor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/INS-JVidal/claude-hooks-monitor/releases/tag/v0.1.0
