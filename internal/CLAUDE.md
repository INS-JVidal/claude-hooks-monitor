# internal

Subpackages:
- hookevt/ — Shared HookEvent type (wire format)
- config/ — INI config parsing, hook toggles, sink config, cache config, atomic file writes
- filecache/ — Session-scoped file read tracking for dedup annotations
- monitor/ — Core event storage ring buffer, console logging, file cache integration
- server/ — HTTP handlers and middleware (hook endpoints, stats, auth)
- sink/ — Outbound event forwarding (EventSink interface, HTTPSink)
- platform/ — OS-specific lock, signals, and instance detection
- tui/ — Bubble Tea interactive tree UI (sessions, events, detail pane, hooks menu)
