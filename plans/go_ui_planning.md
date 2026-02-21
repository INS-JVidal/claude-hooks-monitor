# Claude Code Hooks Monitor - Interactive Tree UI Design Document

**Version:** 3.0  
**Status:** Design Phase  
**Target:** Go Terminal UI Application  
**Purpose:** Real-time hierarchical visualization of Claude Code hook events with interactive navigation

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [System Architecture](#system-architecture)
3. [Data Model](#data-model)
4. [Processing Pipeline](#processing-pipeline)
5. [UI Framework & Technologies](#ui-framework--technologies)
6. [Component Architecture](#component-architecture)
7. [View Hierarchy](#view-hierarchy)
8. [Navigation & Interaction](#navigation--interaction)
9. [Rendering Strategy](#rendering-strategy)
10. [Performance Considerations](#performance-considerations)
11. [State Management](#state-management)
12. [Error Handling & Edge Cases](#error-handling--edge-cases)
13. [Testing Strategy](#testing-strategy)
14. [Implementation Phases](#implementation-phases)
15. [Open Questions](#open-questions)

---

## Executive Summary

### Vision

Transform the current flat, chronological hook monitor into an **interactive terminal UI** that presents events as a **navigable hierarchical tree**, allowing students to:

- See user requests as top-level nodes
- Drill down into tool executions, permissions, subagents
- Toggle between summary and detailed views
- Navigate with keyboard or mouse
- Understand Claude's behavior patterns
- Learn from failures in context

### Key Features

- **Live streaming:** Events arrive continuously, UI updates in real-time
- **Tree navigation:** Hierarchical display with expand/collapse
- **Multi-level detail:** Overview → Timeline → Detail → Raw JSON
- **Smart grouping:** Events grouped by user request
- **Performance:** Handle hundreds of events without lag
- **Educational:** Highlight failures, show recovery patterns, statistics

### Technology Stack

- **Language:** Go 1.21+
- **TUI Framework:** Bubble Tea (Charm ecosystem)
- **Components:** Bubbles (list, viewport, etc.)
- **Styling:** Lipgloss
- **Tree Rendering:** Custom tree component
- **Data Processing:** Concurrent pipelines with channels

---

## System Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Claude Code CLI                         │
└─────────────────────┬───────────────────────────────────────┘
                      │ Hooks fire
                      ↓
┌─────────────────────────────────────────────────────────────┐
│              Hook Client (Go Binary)                        │
│  Sends JSON events via HTTP POST                            │
└─────────────────────┬───────────────────────────────────────┘
                      │ HTTP
                      ↓
┌─────────────────────────────────────────────────────────────┐
│              Monitor Server (HTTP API)                      │
│  - Receives hook events                                     │
│  - Broadcasts to connected clients                          │
└─────────────────────┬───────────────────────────────────────┘
                      │ SSE/WebSocket/Channel
                      ↓
┌─────────────────────────────────────────────────────────────┐
│          Interactive Tree UI (This Application)             │
│                                                              │
│  ┌────────────────┐  ┌──────────────┐  ┌─────────────────┐ │
│  │ Event Receiver │→ │  Processor   │→ │  Tree Builder   │ │
│  └────────────────┘  └──────────────┘  └─────────────────┘ │
│                             ↓                                │
│  ┌────────────────┐  ┌──────────────┐  ┌─────────────────┐ │
│  │   State Mgr    │← │ UI Renderer  │← │ Navigation Ctrl │ │
│  └────────────────┘  └──────────────┘  └─────────────────┘ │
│                             ↓                                │
│                    ┌──────────────┐                          │
│                    │   Terminal   │                          │
│                    └──────────────┘                          │
└─────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

**Event Receiver:**
- Listens for incoming hook events (HTTP SSE, WebSocket, or polling)
- Deserializes JSON to Go structs
- Pushes events to processing queue (buffered channel)

**Processor:**
- Groups events by session and user request
- Detects parent-child relationships
- Infers missing data (subagent start times)
- Calculates statistics (duration, counts, etc.)
- Thread-safe event accumulation

**Tree Builder:**
- Constructs hierarchical data structure
- Maintains order (chronological)
- Supports incremental updates (new events append)
- Handles event correlation (Pre → Post pairs)

**State Manager:**
- Holds current application state
- Manages selected node, expanded nodes, view mode
- Provides state queries for rendering
- Immutable updates (copy-on-write for safety)

**UI Renderer:**
- Renders tree to terminal using Bubble Tea
- Applies styles with Lipgloss
- Handles viewport scrolling
- Manages multiple view modes

**Navigation Controller:**
- Processes keyboard/mouse input
- Translates to state changes
- Implements tree traversal logic
- Handles expand/collapse/navigation

---

## Data Model

### Core Entities

```go
// Session represents a complete Claude Code session
type Session struct {
    ID             string
    ProjectDir     string
    StartTime      time.Time
    EndTime        *time.Time
    Requests       []*UserRequest
    Stats          SessionStats
}

// UserRequest is a top-level user prompt
type UserRequest struct {
    ID            string  // Generated or from first event
    SessionID     string
    Timestamp     time.Time
    Prompt        string
    Events        []Event  // All events in chronological order
    Subagents     []*Subagent
    Stats         RequestStats
    
    // Navigation state (not persisted)
    Expanded      bool
    Selected      bool
}

// Event is the interface for all hook events
type Event interface {
    GetType() EventType
    GetTimestamp() time.Time
    GetID() string
    GetParentID() *string  // For nesting (e.g., PostToolUse parent is PreToolUse)
    GetSummary() string
    GetDetail() EventDetail
    GetRawJSON() json.RawMessage
    
    // Navigation state
    IsExpanded() bool
    SetExpanded(bool)
    IsSelected() bool
    SetSelected(bool)
}

// EventType enum
type EventType int

const (
    EventTypeUserPromptSubmit EventType = iota
    EventTypePreToolUse
    EventTypePostToolUse
    EventTypePostToolUseFailure
    EventTypePermissionRequest
    EventTypeNotification
    EventTypeStop
    EventTypeSubagentStop
    EventTypeSessionStart
    EventTypeSessionEnd
)

// Concrete event implementations
type PreToolUseEvent struct {
    BaseEvent
    ToolName        string
    ToolInput       map[string]interface{}
    RelatedPostEvent *PostToolUseEvent  // Linked when PostToolUse arrives
}

type PostToolUseEvent struct {
    BaseEvent
    ToolName        string
    ToolResponse    map[string]interface{}
    Duration        time.Duration
    ParentPreEvent  *PreToolUseEvent  // Back reference
}

type PostToolUseFailureEvent struct {
    BaseEvent
    ToolName        string
    Error           string
    ErrorCode       int
    IsInterrupt     bool
    ParentPreEvent  *PreToolUseEvent
}

type PermissionRequestEvent struct {
    BaseEvent
    Suggestions     []PermissionSuggestion
    ChildEvents     []Event  // Notifications that followed
}

type Subagent struct {
    ID              string
    StartTime       time.Time  // Inferred from first mention
    StopTime        *time.Time
    LastMessage     string
    Events          []Event  // If we track subagent-specific events
    ParentRequest   *UserRequest
}

// EventDetail provides structured view of event data
type EventDetail struct {
    Title           string
    Fields          []DetailField
    Actions         []DetailAction  // e.g., "Copy JSON", "Export"
}

type DetailField struct {
    Label           string
    Value           string
    Type            FieldType  // Text, Code, JSON, Duration, etc.
    Highlight       bool       // For important fields
}

// Statistics
type SessionStats struct {
    TotalRequests   int
    TotalTools      int
    SuccessfulTools int
    FailedTools     int
    TotalDuration   time.Duration
    Subagents       int
}

type RequestStats struct {
    Duration        time.Duration
    ToolCount       int
    SuccessCount    int
    FailureCount    int
    PermissionCount int
    SubagentCount   int
    ToolBreakdown   map[string]ToolStats  // Per tool name
}

type ToolStats struct {
    ToolName        string
    TotalCalls      int
    Successes       int
    Failures        int
    AvgDuration     time.Duration
}
```

### Tree Node Structure

```go
// TreeNode is the UI representation (wraps domain entities)
type TreeNode struct {
    ID              string
    Type            NodeType  // Session, Request, Event, Subagent
    Label           string    // Display text
    Data            interface{}  // Underlying entity (Session, UserRequest, Event, etc.)
    
    Children        []*TreeNode
    Parent          *TreeNode
    
    // UI State
    Expanded        bool
    Selected        bool
    Depth           int  // Indentation level
    Index           int  // Position in flat list (for navigation)
    
    // Rendering
    Icon            string  // Emoji or symbol
    Style           lipgloss.Style
    HighlightReason string  // Why highlighted (error, warning, etc.)
}

type NodeType int

const (
    NodeTypeSession NodeType = iota
    NodeTypeRequest
    NodeTypeEvent
    NodeTypeSubagent
    NodeTypeStatistics
)
```

### Flat Index for Navigation

```go
// FlatNodeList is a flattened view of the tree for efficient navigation
type FlatNodeList struct {
    Nodes           []*TreeNode  // Visible nodes in render order
    SelectedIndex   int
    
    // Quick lookup
    NodeByID        map[string]*TreeNode
    
    // For viewport scrolling
    ViewportTop     int
    ViewportHeight  int
}

// Rebuilt whenever tree expands/collapses
func (t *Tree) Flatten() *FlatNodeList {
    // Walk tree, collect visible nodes
    // Assign indices sequentially
    // Update parent references
}
```

---

## Processing Pipeline

### Event Ingestion Flow

```
HTTP SSE Stream → Event Buffer → Processor → Tree Update → UI Render
    (async)      (channel 100)   (goroutine)   (mutex)    (60 FPS max)
```

### Event Processor Design

```go
type EventProcessor struct {
    // Input
    eventQueue      chan RawEvent
    
    // State
    sessions        map[string]*Session
    currentSession  *Session
    currentRequest  *UserRequest
    pendingPreTools map[string]*PreToolUseEvent  // Waiting for Post
    
    // Output
    treeUpdateChan  chan TreeUpdate
    
    // Synchronization
    mu              sync.RWMutex
}

type RawEvent struct {
    Type            string
    Timestamp       time.Time
    Data            json.RawMessage
}

type TreeUpdate struct {
    Type            UpdateType  // Add, Modify, Complete
    SessionID       string
    RequestID       string
    EventID         string
    Node            *TreeNode
}

func (p *EventProcessor) Start() {
    go func() {
        for raw := range p.eventQueue {
            p.processEvent(raw)
        }
    }()
}

func (p *EventProcessor) processEvent(raw RawEvent) {
    // 1. Deserialize to concrete type
    event := p.deserialize(raw)
    
    // 2. Determine session (create if new)
    session := p.getOrCreateSession(event)
    
    // 3. Determine request (group by UserPromptSubmit or time proximity)
    request := p.getOrCreateRequest(session, event)
    
    // 4. Add event to request
    request.Events = append(request.Events, event)
    
    // 5. Link related events (Pre/Post pairs)
    p.linkRelatedEvents(event)
    
    // 6. Update statistics
    p.updateStats(request, event)
    
    // 7. Notify tree builder
    p.treeUpdateChan <- TreeUpdate{
        Type: UpdateTypeAdd,
        SessionID: session.ID,
        RequestID: request.ID,
        EventID: event.GetID(),
        Node: p.buildNode(event),
    }
}

func (p *EventProcessor) linkRelatedEvents(event Event) {
    switch e := event.(type) {
    case *PostToolUseEvent:
        // Find matching PreToolUse
        if pre, ok := p.pendingPreTools[e.ToolUseID]; ok {
            e.ParentPreEvent = pre
            pre.RelatedPostEvent = e
            delete(p.pendingPreTools, e.ToolUseID)
        }
        
    case *PreToolUseEvent:
        // Store for future Post
        p.pendingPreTools[e.ToolUseID] = e
        
    case *PermissionRequestEvent:
        // Store for potential Notification child
        // (handled by request grouping)
    }
}
```

### Request Grouping Strategy

**Rules:**
1. `UserPromptSubmit` starts a new request
2. Events within 5 seconds of prompt belong to that request
3. `Stop` or `SessionEnd` closes current request
4. `SubagentStop` attaches to most recent request
5. Events before first prompt go to "Initial Setup" pseudo-request

```go
func (p *EventProcessor) getOrCreateRequest(session *Session, event Event) *UserRequest {
    switch event.GetType() {
    case EventTypeUserPromptSubmit:
        // Always start new request
        req := &UserRequest{
            ID: generateID(),
            SessionID: session.ID,
            Timestamp: event.GetTimestamp(),
            Prompt: event.(*UserPromptSubmitEvent).Prompt,
        }
        session.Requests = append(session.Requests, req)
        p.currentRequest = req
        return req
        
    case EventTypeStop:
        // Close current request
        if p.currentRequest != nil {
            p.currentRequest.EndTime = event.GetTimestamp()
        }
        return p.currentRequest
        
    default:
        // Belongs to current request
        if p.currentRequest == nil {
            // Create implicit request
            p.currentRequest = &UserRequest{
                ID: "initial",
                SessionID: session.ID,
                Timestamp: session.StartTime,
                Prompt: "(Initial setup)",
            }
            session.Requests = append(session.Requests, p.currentRequest)
        }
        return p.currentRequest
    }
}
```

---

## UI Framework & Technologies

### Bubble Tea Architecture

**Why Bubble Tea:**
- De facto standard for Go TUI apps
- Elm-inspired architecture (Model-Update-View)
- Excellent performance
- Strong ecosystem (Bubbles, Lipgloss)
- Mouse support built-in

**Core Pattern:**

```go
type Model struct {
    // Data
    tree            *Tree
    sessions        []*Session
    
    // UI State
    viewMode        ViewMode
    selectedNode    *TreeNode
    flatList        *FlatNodeList
    
    // Components
    viewport        viewport.Model
    help            help.Model
    
    // Dimensions
    width           int
    height          int
    
    // Status
    loading         bool
    error           error
}

func (m Model) Init() tea.Cmd {
    return listenForEvents()  // Start event receiver
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return m.handleKeyPress(msg)
    case tea.MouseMsg:
        return m.handleMouse(msg)
    case EventReceivedMsg:
        return m.handleNewEvent(msg)
    case tea.WindowSizeMsg:
        return m.handleResize(msg)
    }
    return m, nil
}

func (m Model) View() string {
    return m.renderCurrentView()
}
```

### Component Library

**Bubbles Components Used:**

1. **Viewport** - Scrollable content area
   - Shows tree content
   - Handles overflow
   - Smooth scrolling

2. **Help** - Keyboard shortcuts display
   - Context-sensitive
   - Toggle visibility

3. **Custom Tree Component** (build ourselves)
   - Recursive rendering
   - Expand/collapse
   - Selection highlighting

**Lipgloss Styling:**

```go
var (
    baseStyle = lipgloss.NewStyle().
        BorderStyle(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("240"))
    
    selectedStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("229")).
        Background(lipgloss.Color("57")).
        Bold(true)
    
    errorStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("196")).
        Bold(true)
    
    successStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("42"))
    
    timestampStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("241"))
    
    promptStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("229")).
        Bold(true)
)
```

---

## Component Architecture

### Tree Component

**Responsibilities:**
- Render hierarchical structure
- Handle expand/collapse
- Track selection
- Provide navigation API

```go
type TreeComponent struct {
    root            *TreeNode
    flatList        *FlatNodeList
    selectedIndex   int
    
    // Rendering config
    indentSize      int
    showIcons       bool
    expandIcon      string  // "▼"
    collapseIcon    string  // "▶"
    
    // Viewport integration
    viewport        viewport.Model
}

func (t *TreeComponent) Render() string {
    var lines []string
    
    for i, node := range t.flatList.Nodes {
        line := t.renderNode(node, i == t.selectedIndex)
        lines = append(lines, line)
    }
    
    content := strings.Join(lines, "\n")
    t.viewport.SetContent(content)
    return t.viewport.View()
}

func (t *TreeComponent) renderNode(node *TreeNode, selected bool) string {
    // Indentation
    indent := strings.Repeat("  ", node.Depth)
    
    // Expand/collapse icon
    icon := ""
    if len(node.Children) > 0 {
        if node.Expanded {
            icon = t.expandIcon
        } else {
            icon = t.collapseIcon
        }
    } else {
        icon = " "  // Spacing
    }
    
    // Node icon (emoji)
    nodeIcon := node.Icon
    
    // Label
    label := node.Label
    
    // Apply styling
    style := lipgloss.NewStyle()
    if selected {
        style = selectedStyle
    } else if node.HighlightReason != "" {
        switch node.HighlightReason {
        case "error":
            style = errorStyle
        case "warning":
            style = warningStyle
        }
    }
    
    line := fmt.Sprintf("%s%s %s %s", indent, icon, nodeIcon, label)
    return style.Render(line)
}

func (t *TreeComponent) Navigate(direction NavDirection) {
    switch direction {
    case NavUp:
        if t.selectedIndex > 0 {
            t.selectedIndex--
            t.ensureVisible()
        }
    case NavDown:
        if t.selectedIndex < len(t.flatList.Nodes)-1 {
            t.selectedIndex++
            t.ensureVisible()
        }
    case NavLeft:
        // Collapse current or go to parent
        node := t.flatList.Nodes[t.selectedIndex]
        if node.Expanded {
            t.CollapseNode(node)
        } else if node.Parent != nil {
            t.SelectNode(node.Parent)
        }
    case NavRight:
        // Expand current
        node := t.flatList.Nodes[t.selectedIndex]
        if len(node.Children) > 0 && !node.Expanded {
            t.ExpandNode(node)
        }
    }
}

func (t *TreeComponent) ensureVisible() {
    // Adjust viewport to show selected node
    selectedNode := t.flatList.Nodes[t.selectedIndex]
    
    if selectedNode.Index < t.viewport.YOffset {
        // Scroll up
        t.viewport.YOffset = selectedNode.Index
    } else if selectedNode.Index >= t.viewport.YOffset + t.viewport.Height {
        // Scroll down
        t.viewport.YOffset = selectedNode.Index - t.viewport.Height + 1
    }
}
```

### Detail View Component

**Responsibilities:**
- Show event details in structured format
- Provide actions (copy, export)
- Format different data types

```go
type DetailView struct {
    event           Event
    width           int
    height          int
    
    // Sections
    header          string
    fields          []DetailField
    actions         []Action
    
    // State
    selectedAction  int
}

func (d *DetailView) Render() string {
    var sections []string
    
    // Header
    header := d.renderHeader()
    sections = append(sections, header)
    
    // Fields
    for _, field := range d.fields {
        fieldStr := d.renderField(field)
        sections = append(sections, fieldStr)
    }
    
    // Actions
    if len(d.actions) > 0 {
        actionsStr := d.renderActions()
        sections = append(sections, actionsStr)
    }
    
    // Box it
    content := strings.Join(sections, "\n")
    box := boxStyle.Width(d.width - 4).Render(content)
    return box
}

func (d *DetailView) renderField(field DetailField) string {
    label := labelStyle.Render(field.Label + ":")
    
    var value string
    switch field.Type {
    case FieldTypeJSON:
        // Pretty print JSON
        var formatted bytes.Buffer
        json.Indent(&formatted, []byte(field.Value), "", "  ")
        value = codeStyle.Render(formatted.String())
        
    case FieldTypeDuration:
        // Human readable duration
        dur, _ := time.ParseDuration(field.Value)
        value = durationStyle.Render(formatDuration(dur))
        
    case FieldTypeCode:
        value = codeStyle.Render(field.Value)
        
    default:
        value = field.Value
    }
    
    if field.Highlight {
        value = highlightStyle.Render(value)
    }
    
    return fmt.Sprintf("%s %s", label, value)
}
```

### Statistics Panel Component

```go
type StatsPanel struct {
    request         *UserRequest
    width           int
}

func (s *StatsPanel) Render() string {
    stats := s.request.Stats
    
    lines := []string{
        titleStyle.Render("Request Summary"),
        "",
        fmt.Sprintf("Duration: %s", formatDuration(stats.Duration)),
        "",
        "Activity:",
        fmt.Sprintf("  Tools executed: %d", stats.ToolCount),
        fmt.Sprintf("    ✓ Successful: %d", stats.SuccessCount),
        fmt.Sprintf("    ✗ Failed: %d", stats.FailureCount),
        fmt.Sprintf("  Permissions: %d", stats.PermissionCount),
        fmt.Sprintf("  Subagents: %d", stats.SubagentCount),
        "",
        "Tools Used:",
    }
    
    for toolName, toolStats := range stats.ToolBreakdown {
        line := fmt.Sprintf("  %s: %d times (%d ✓, %d ✗)",
            toolName, toolStats.TotalCalls, 
            toolStats.Successes, toolStats.Failures)
        lines = append(lines, line)
    }
    
    // Outcome
    outcome := s.determineOutcome()
    lines = append(lines, "", "Outcome:", "  "+outcome)
    
    content := strings.Join(lines, "\n")
    return boxStyle.Width(s.width).Render(content)
}
```

---

## View Hierarchy

### View Modes

```go
type ViewMode int

const (
    ViewModeTree ViewMode = iota       // Hierarchical tree
    ViewModeDetail                      // Focused detail view
    ViewModeJSON                        // Raw JSON view
    ViewModeStats                       // Statistics overview
    ViewModeHelp                        // Help screen
)
```

### View Transitions

```
TreeView (default)
  ├─ Press 'Enter' on node → DetailView
  ├─ Press 'f' on node → JSONView
  ├─ Press 's' on request → StatsView
  └─ Press '?' → HelpView

DetailView
  ├─ Press 'f' → JSONView
  ├─ Press Esc/← → TreeView
  └─ Press 's' → StatsView (if on request)

JSONView
  ├─ Press Esc/← → DetailView or TreeView
  └─ Press 'c' → Copy to clipboard

StatsView
  ├─ Press Esc/← → TreeView
  └─ Press 't' → TreeView (timeline)

HelpView
  └─ Press any key → return to previous view
```

### Layout Structure

```
┌─────────────────────────────────────────────────────────────┐
│ Header: Session Info, Mode Indicator           [Status Bar]│
├─────────────────────────────────────────────────────────────┤
│                                                              │
│                       Main Content Area                      │
│                     (TreeView / DetailView)                  │
│                                                              │
│                                                              │
│                                                              │
│                                                              │
├─────────────────────────────────────────────────────────────┤
│ Footer: Help Hints, Shortcuts                  [Time/Stats]│
└─────────────────────────────────────────────────────────────┘
```

**Responsive Layout:**

```go
func (m Model) View() string {
    // Header
    header := m.renderHeader()
    
    // Main content (changes by mode)
    var content string
    switch m.viewMode {
    case ViewModeTree:
        content = m.tree.Render()
    case ViewModeDetail:
        content = m.detailView.Render()
    case ViewModeJSON:
        content = m.jsonView.Render()
    case ViewModeStats:
        content = m.statsPanel.Render()
    case ViewModeHelp:
        content = m.help.View(m.keys)
    }
    
    // Footer
    footer := m.renderFooter()
    
    // Combine with proper heights
    headerHeight := lipgloss.Height(header)
    footerHeight := lipgloss.Height(footer)
    contentHeight := m.height - headerHeight - footerHeight - 2  // Borders
    
    content = m.viewport.View()  // Apply viewport for scrolling
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        content,
        footer,
    )
}
```

---

## Navigation & Interaction

### Keyboard Bindings

```go
type KeyMap struct {
    // Navigation
    Up              key.Binding
    Down            key.Binding
    Left            key.Binding  // Collapse or parent
    Right           key.Binding  // Expand
    
    PageUp          key.Binding
    PageDown        key.Binding
    Home            key.Binding
    End             key.Binding
    
    // Actions
    Enter           key.Binding  // Expand/collapse or detail
    Space           key.Binding  // Quick preview
    
    // View switching
    Detail          key.Binding  // 'd' - detail view
    JSON            key.Binding  // 'f' - full JSON
    Stats           key.Binding  // 's' - statistics
    Back            key.Binding  // Esc, ← - go back
    
    // Filtering
    Search          key.Binding  // '/' - search
    ErrorsOnly      key.Binding  // 'e' - show errors
    FilterTool      key.Binding  // 't' - filter by tool
    AllSubagents    key.Binding  // 'a' - all subagents
    
    // Utilities
    Copy            key.Binding  // 'c' - copy JSON
    Export          key.Binding  // 'x' - export request
    ExportAll       key.Binding  // 'X' - export session
    Help            key.Binding  // '?' - help screen
    Quit            key.Binding  // 'q' - quit
    
    // View modes
    ToggleTimestamps key.Binding  // 'T' - show/hide timestamps
    ToggleIcons      key.Binding  // 'I' - show/hide icons
    CompactMode      key.Binding  // 'C' - compact view
}

var DefaultKeyMap = KeyMap{
    Up:      key.NewBinding(key.WithKeys("up", "k")),
    Down:    key.NewBinding(key.WithKeys("down", "j")),
    Left:    key.NewBinding(key.WithKeys("left", "h")),
    Right:   key.NewBinding(key.WithKeys("right", "l")),
    
    PageUp:  key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
    PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
    Home:    key.NewBinding(key.WithKeys("home", "g")),
    End:     key.NewBinding(key.WithKeys("end", "G")),
    
    Enter:   key.NewBinding(key.WithKeys("enter")),
    Space:   key.NewBinding(key.WithKeys(" ")),
    
    Detail:  key.NewBinding(key.WithKeys("d")),
    JSON:    key.NewBinding(key.WithKeys("f")),
    Stats:   key.NewBinding(key.WithKeys("s")),
    Back:    key.NewBinding(key.WithKeys("esc", "backspace")),
    
    Search:  key.NewBinding(key.WithKeys("/")),
    // ... etc
}
```

### Mouse Support

```go
func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
    switch msg.Type {
    case tea.MouseLeft:
        // Click to select
        clickY := msg.Y - m.headerHeight
        nodeIndex := m.viewport.YOffset + clickY
        
        if nodeIndex >= 0 && nodeIndex < len(m.flatList.Nodes) {
            m.selectedIndex = nodeIndex
            return m, nil
        }
        
    case tea.MouseWheelUp:
        // Scroll up
        m.viewport.LineUp(3)
        return m, nil
        
    case tea.MouseWheelDown:
        // Scroll down
        m.viewport.LineDown(3)
        return m, nil
        
    case tea.MouseRight:
        // Context menu (future)
        return m.showContextMenu()
    }
    
    return m, nil
}
```

### Context-Sensitive Help

```go
func (m Model) getHelpKeys() []key.Binding {
    var keys []key.Binding
    
    switch m.viewMode {
    case ViewModeTree:
        keys = []key.Binding{
            m.keyMap.Up, m.keyMap.Down,
            m.keyMap.Left, m.keyMap.Right,
            m.keyMap.Enter, m.keyMap.Detail,
            m.keyMap.Search, m.keyMap.Help,
        }
        
    case ViewModeDetail:
        keys = []key.Binding{
            m.keyMap.JSON, m.keyMap.Stats,
            m.keyMap.Copy, m.keyMap.Back,
        }
        
    case ViewModeJSON:
        keys = []key.Binding{
            m.keyMap.Copy, m.keyMap.Export,
            m.keyMap.Back,
        }
    }
    
    return keys
}
```

---

## Rendering Strategy

### Efficient Rendering

**Challenge:** Large trees (100+ nodes) must render smoothly

**Solutions:**

1. **Virtual Scrolling** - Only render visible nodes
   ```go
   func (t *TreeComponent) getVisibleNodes() []*TreeNode {
       start := t.viewport.YOffset
       end := start + t.viewport.Height
       
       if end > len(t.flatList.Nodes) {
           end = len(t.flatList.Nodes)
       }
       
       return t.flatList.Nodes[start:end]
   }
   ```

2. **Incremental Updates** - Only re-render changed nodes
   ```go
   type RenderCache struct {
       renderedLines   map[int]string  // Line number → rendered string
       dirty           map[int]bool    // Which lines need re-render
   }
   
   func (t *TreeComponent) renderWithCache() string {
       visible := t.getVisibleNodes()
       var lines []string
       
       for i, node := range visible {
           lineNum := node.Index
           
           if t.cache.dirty[lineNum] || t.cache.renderedLines[lineNum] == "" {
               // Re-render this line
               line := t.renderNode(node, node.Selected)
               t.cache.renderedLines[lineNum] = line
               t.cache.dirty[lineNum] = false
           }
           
           lines = append(lines, t.cache.renderedLines[lineNum])
       }
       
       return strings.Join(lines, "\n")
   }
   ```

3. **Debounced Re-renders** - Batch rapid updates
   ```go
   func (m Model) handleNewEvent(event Event) (Model, tea.Cmd) {
       // Add to buffer
       m.pendingEvents = append(m.pendingEvents, event)
       
       // Start debounce timer if not running
       if !m.debounceActive {
           m.debounceActive = true
           return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
               return FlushEventsMsg{}
           })
       }
       
       return m, nil
   }
   
   func (m Model) flushEvents() (Model, tea.Cmd) {
       // Process all pending events
       for _, event := range m.pendingEvents {
           m.processor.processEvent(event)
       }
       
       // Rebuild tree
       m.tree.Rebuild()
       m.flatList = m.tree.Flatten()
       
       m.pendingEvents = nil
       m.debounceActive = false
       
       return m, nil
   }
   ```

4. **Frame Rate Limiting** - Max 60 FPS
   ```go
   const TargetFPS = 60
   const FrameDuration = time.Second / TargetFPS
   
   func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
       now := time.Now()
       if now.Sub(m.lastRender) < FrameDuration {
           // Skip render, too soon
           return m, nil
       }
       
       m.lastRender = now
       // ... normal update
   }
   ```

### Progressive Loading

For very large sessions:

```go
type LazyLoadingTree struct {
    loadedDepth     int  // How many levels loaded
    totalDepth      int  // Total depth available
    
    // Load more when scrolling near bottom
    loadThreshold   int  // Lines from bottom to trigger load
}

func (t *LazyLoadingTree) shouldLoadMore() bool {
    distanceToBottom := len(t.flatList.Nodes) - t.viewport.YOffset - t.viewport.Height
    return distanceToBottom < t.loadThreshold
}

func (t *LazyLoadingTree) loadNextLevel() {
    // Expand one more level of depth
    t.loadedDepth++
    t.expandToDepth(t.loadedDepth)
    t.flatList = t.flatten()
}
```

---

## Performance Considerations

### Memory Management

**Concerns:**
- Long-running sessions = many events
- Full JSON stored in memory
- Tree nodes duplicate data

**Strategies:**

1. **Event Pruning** - Keep last N requests
   ```go
   const MaxRequestsInMemory = 100
   
   func (s *Session) pruneOldRequests() {
       if len(s.Requests) > MaxRequestsInMemory {
           // Keep most recent
           oldRequests := s.Requests[:len(s.Requests)-MaxRequestsInMemory]
           
           // Archive to disk (optional)
           s.archiveRequests(oldRequests)
           
           // Remove from memory
           s.Requests = s.Requests[len(s.Requests)-MaxRequestsInMemory:]
       }
   }
   ```

2. **JSON Lazy Parsing** - Parse on demand
   ```go
   type Event struct {
       Type            EventType
       Timestamp       time.Time
       Summary         string  // Pre-parsed for display
       
       rawJSON         json.RawMessage  // Stored as-is
       parsedDetail    *EventDetail     // Nil until needed
       parsedDetailOnce sync.Once
   }
   
   func (e *Event) GetDetail() *EventDetail {
       e.parsedDetailOnce.Do(func() {
           e.parsedDetail = parseEventDetail(e.rawJSON)
       })
       return e.parsedDetail
   }
   ```

3. **Object Pooling** - Reuse tree nodes
   ```go
   var treeNodePool = sync.Pool{
       New: func() interface{} {
           return &TreeNode{}
       },
   }
   
   func acquireTreeNode() *TreeNode {
       return treeNodePool.Get().(*TreeNode)
   }
   
   func releaseTreeNode(n *TreeNode) {
       // Reset state
       n.Children = nil
       n.Parent = nil
       n.Data = nil
       // Return to pool
       treeNodePool.Put(n)
   }
   ```

### Concurrency

**Event Processing:**
- Goroutine receives events from HTTP stream
- Buffered channel (100 capacity) prevents blocking
- Single processor goroutine ensures serial processing
- Tree updates sent via channel to UI goroutine

```go
func startEventPipeline() {
    eventQueue := make(chan RawEvent, 100)
    treeUpdates := make(chan TreeUpdate, 50)
    
    // Event receiver (HTTP client)
    go receiveEvents(eventQueue)
    
    // Event processor
    go processEvents(eventQueue, treeUpdates)
    
    // UI updates happen in main goroutine via Bubble Tea
}
```

**Thread Safety:**

```go
type SafeSession struct {
    session     *Session
    mu          sync.RWMutex
}

func (s *SafeSession) AddRequest(req *UserRequest) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.session.Requests = append(s.session.Requests, req)
}

func (s *SafeSession) GetRequests() []*UserRequest {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    // Return copy to prevent concurrent modification
    requests := make([]*UserRequest, len(s.session.Requests))
    copy(requests, s.session.Requests)
    return requests
}
```

---

## State Management

### Application State

```go
type AppState struct {
    // Data layer
    sessions        map[string]*SafeSession
    currentSession  *Session
    
    // UI layer
    tree            *Tree
    flatList        *FlatNodeList
    selectedNodeID  string
    expandedNodeIDs map[string]bool
    
    // View state
    viewMode        ViewMode
    viewHistory     []ViewMode  // For back navigation
    
    // Filters
    searchQuery     string
    activeFilters   []Filter
    
    // Preferences
    showTimestamps  bool
    showIcons       bool
    compactMode     bool
    
    mu              sync.RWMutex
}

type Filter struct {
    Type            FilterType
    Value           string
}

type FilterType int

const (
    FilterTypeToolName FilterType = iota
    FilterTypeEventType
    FilterTypeErrorsOnly
    FilterTypeSessionID
)
```

### State Transitions

```go
func (s *AppState) ApplyUpdate(update StateUpdate) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    switch update := update.(type) {
    case *NodeExpandUpdate:
        s.expandedNodeIDs[update.NodeID] = true
        s.rebuildFlatList()
        
    case *NodeCollapseUpdate:
        delete(s.expandedNodeIDs, update.NodeID)
        s.rebuildFlatList()
        
    case *ViewModeUpdate:
        s.viewHistory = append(s.viewHistory, s.viewMode)
        s.viewMode = update.NewMode
        
    case *FilterUpdate:
        s.activeFilters = update.Filters
        s.rebuildFlatList()  // Re-filter visible nodes
        
    case *NewEventUpdate:
        session := s.getOrCreateSession(update.Event.SessionID)
        session.AddEvent(update.Event)
        s.tree.AddEvent(update.Event)
        s.rebuildFlatList()
    }
}

func (s *AppState) rebuildFlatList() {
    // Walk expanded tree, apply filters, build flat list
    s.flatList = s.tree.FlattenWithFilters(s.activeFilters, s.expandedNodeIDs)
}
```

### Persistence (Optional)

```go
type StatePersistence struct {
    stateFile   string
}

func (p *StatePersistence) Save(state *AppState) error {
    data := map[string]interface{}{
        "expanded_nodes": state.expandedNodeIDs,
        "view_mode": state.viewMode,
        "preferences": map[string]bool{
            "show_timestamps": state.showTimestamps,
            "show_icons": state.showIcons,
            "compact_mode": state.compactMode,
        },
    }
    
    file, err := os.Create(p.stateFile)
    if err != nil {
        return err
    }
    defer file.Close()
    
    return json.NewEncoder(file).Encode(data)
}

func (p *StatePersistence) Load() (*AppState, error) {
    // Load saved state
    // Used for session resume
}
```

---

## Error Handling & Edge Cases

### Event Processing Errors

```go
func (p *EventProcessor) processEvent(raw RawEvent) {
    defer func() {
        if r := recover(); r != nil {
            // Log error, don't crash
            log.Printf("Panic processing event: %v\n%s", r, debug.Stack())
            
            // Create error event to show in UI
            p.treeUpdateChan <- TreeUpdate{
                Type: UpdateTypeError,
                Node: &TreeNode{
                    Label: fmt.Sprintf("Error processing event: %v", r),
                    Icon: "⚠️",
                    Style: errorStyle,
                },
            }
        }
    }()
    
    // ... process event
}
```

### Network Failures

```go
func (r *EventReceiver) Start() {
    for {
        err := r.connectAndListen()
        if err != nil {
            log.Printf("Connection lost: %v", err)
            
            // Notify UI
            r.statusChan <- StatusMsg{
                Type: StatusDisconnected,
                Message: "Connection lost. Retrying...",
            }
            
            // Exponential backoff
            time.Sleep(r.reconnectDelay)
            r.reconnectDelay = min(r.reconnectDelay*2, 30*time.Second)
            continue
        }
        
        // Reset backoff on success
        r.reconnectDelay = 1 * time.Second
    }
}
```

### Malformed Events

```go
func (p *EventProcessor) deserialize(raw RawEvent) (Event, error) {
    var base BaseEvent
    if err := json.Unmarshal(raw.Data, &base); err != nil {
        // Create placeholder event
        return &ErrorEvent{
            Timestamp: raw.Timestamp,
            Error: fmt.Sprintf("Failed to parse: %v", err),
            RawData: raw.Data,
        }, nil
    }
    
    // Type-specific deserialization
    switch base.Type {
    case "PreToolUse":
        var event PreToolUseEvent
        if err := json.Unmarshal(raw.Data, &event); err != nil {
            return &ErrorEvent{...}, nil
        }
        return &event, nil
    // ... etc
    }
}
```

### Edge Cases

**1. Events Arrive Out of Order**
```go
// Solution: Buffer events, sort by timestamp before processing
type EventBuffer struct {
    events      []RawEvent
    maxAge      time.Duration  // Max time to buffer
    lastFlush   time.Time
}

func (b *EventBuffer) Add(event RawEvent) {
    b.events = append(b.events, event)
    
    // Flush if buffer full or old
    if len(b.events) > 100 || time.Since(b.lastFlush) > b.maxAge {
        b.Flush()
    }
}

func (b *EventBuffer) Flush() {
    // Sort by timestamp
    sort.Slice(b.events, func(i, j int) bool {
        return b.events[i].Timestamp.Before(b.events[j].Timestamp)
    })
    
    // Process in order
    for _, event := range b.events {
        processor.processEvent(event)
    }
    
    b.events = nil
    b.lastFlush = time.Now()
}
```

**2. PreToolUse Without Matching PostToolUse**
```go
// Solution: Timeout orphaned Pre events
type OrphanedPreToolCleaner struct {
    pendingPre  map[string]*PreToolUseEvent
    timeout     time.Duration
}

func (c *OrphanedPreToolCleaner) CleanUp() {
    now := time.Now()
    
    for id, preEvent := range c.pendingPre {
        if now.Sub(preEvent.Timestamp) > c.timeout {
            // Mark as incomplete
            preEvent.Incomplete = true
            delete(c.pendingPre, id)
        }
    }
}
```

**3. Session Never Ends**
```go
// Solution: Auto-close sessions after inactivity
func (s *Session) checkAutoClose() bool {
    if s.EndTime != nil {
        return false  // Already closed
    }
    
    lastActivity := s.getLastActivityTime()
    if time.Since(lastActivity) > 30*time.Minute {
        s.EndTime = &lastActivity
        return true
    }
    
    return false
}
```

---

## Testing Strategy

### Unit Tests

```go
func TestEventProcessing(t *testing.T) {
    processor := NewEventProcessor()
    
    // Test request grouping
    t.Run("groups events by UserPromptSubmit", func(t *testing.T) {
        // Send UserPromptSubmit
        processor.processEvent(createUserPromptEvent("test prompt"))
        
        // Send tool events
        processor.processEvent(createPreToolUseEvent("Bash"))
        processor.processEvent(createPostToolUseEvent("Bash"))
        
        session := processor.GetCurrentSession()
        assert.Equal(t, 1, len(session.Requests))
        assert.Equal(t, 2, len(session.Requests[0].Events))
    })
    
    t.Run("links Pre and Post tool events", func(t *testing.T) {
        preEvent := createPreToolUseEvent("Read")
        processor.processEvent(preEvent)
        
        postEvent := createPostToolUseEvent("Read")
        postEvent.ToolUseID = preEvent.ToolUseID
        processor.processEvent(postEvent)
        
        // Check linkage
        assert.NotNil(t, preEvent.RelatedPostEvent)
        assert.Equal(t, postEvent, preEvent.RelatedPostEvent)
    })
}

func TestTreeNavigation(t *testing.T) {
    tree := createTestTree()
    
    t.Run("navigates down", func(t *testing.T) {
        tree.selectedIndex = 0
        tree.Navigate(NavDown)
        assert.Equal(t, 1, tree.selectedIndex)
    })
    
    t.Run("expands node", func(t *testing.T) {
        node := tree.flatList.Nodes[0]
        tree.Navigate(NavRight)
        assert.True(t, node.Expanded)
    })
    
    t.Run("collapses node", func(t *testing.T) {
        node := tree.flatList.Nodes[0]
        node.Expanded = true
        tree.Navigate(NavLeft)
        assert.False(t, node.Expanded)
    })
}
```

### Integration Tests

```go
func TestEndToEnd(t *testing.T) {
    // Start mock HTTP server
    server := httptest.NewServer(createMockEventStream())
    defer server.Close()
    
    // Start application
    app := NewApplication(server.URL)
    app.Start()
    
    // Wait for events to process
    time.Sleep(100 * time.Millisecond)
    
    // Verify tree structure
    session := app.state.currentSession
    assert.Equal(t, 1, len(session.Requests))
    
    // Verify UI state
    assert.Equal(t, ViewModeTree, app.model.viewMode)
    assert.NotNil(t, app.model.flatList)
    
    app.Stop()
}
```

### Performance Tests

```go
func BenchmarkTreeRendering(b *testing.B) {
    tree := createLargeTree(1000) // 1000 nodes
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = tree.Render()
    }
}

func BenchmarkEventProcessing(b *testing.B) {
    processor := NewEventProcessor()
    events := createTestEvents(100)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for _, event := range events {
            processor.processEvent(event)
        }
    }
}
```

### Visual Regression Tests

```go
// Use go-expect to test terminal output
func TestTreeRendering(t *testing.T) {
    console, err := expect.NewConsole(expect.WithStdout(os.Stdout))
    require.NoError(t, err)
    defer console.Close()
    
    // Run application
    cmd := exec.Command("./monitor-ui")
    cmd.Stdin = console.Tty()
    cmd.Stdout = console.Tty()
    cmd.Stderr = console.Tty()
    
    err = cmd.Start()
    require.NoError(t, err)
    
    // Wait for render
    console.ExpectString("Claude Code Session Monitor")
    
    // Send key press
    console.Send("j") // Navigate down
    
    // Check output
    output, err := console.ExpectString("▼")
    require.NoError(t, err)
    assert.Contains(t, output, "22:47:29")
    
    cmd.Process.Kill()
}
```

---

## Implementation Phases

### Phase 1: Foundation (Week 1-2)

**Goals:**
- Basic data model
- Event receiver
- Simple tree structure

**Deliverables:**
1. Data structures (Session, Request, Events)
2. HTTP/SSE event receiver
3. Basic event processor (grouping by request)
4. In-memory storage
5. Unit tests for core logic

**Success Criteria:**
- Events received and grouped correctly
- Pre/Post events linked
- Session and request tracking works

---

### Phase 2: Tree UI (Week 3-4)

**Goals:**
- Bubble Tea integration
- Tree rendering
- Basic navigation

**Deliverables:**
1. Bubble Tea Model/Update/View
2. Tree component with expand/collapse
3. Keyboard navigation (up/down/left/right)
4. Selection highlighting
5. Viewport scrolling

**Success Criteria:**
- Tree displays correctly
- Navigation is smooth
- Expand/collapse works
- Can scroll through large trees

---

### Phase 3: Detail Views (Week 5-6)

**Goals:**
- Multiple view modes
- Detail rendering
- JSON view

**Deliverables:**
1. Detail view component
2. JSON pretty-printing view
3. Stats panel component
4. View mode switching
5. Breadcrumb navigation

**Success Criteria:**
- Can drill down to details
- JSON displays properly formatted
- Easy to navigate back
- Stats are accurate

---

### Phase 4: Live Updates (Week 7-8)

**Goals:**
- Real-time event streaming
- Efficient updates
- Performance optimization

**Deliverables:**
1. Live event integration with UI
2. Debounced re-renders
3. Virtual scrolling
4. Render caching
5. Memory management

**Success Criteria:**
- UI stays responsive with continuous events
- No lag or stutter
- Memory stays bounded
- 60 FPS maintained

---

### Phase 5: Advanced Features (Week 9-10)

**Goals:**
- Filtering
- Search
- Export
- Polish

**Deliverables:**
1. Search functionality
2. Filter by tool/error/etc
3. Export to JSON/file
4. Copy to clipboard
5. Help system
6. Mouse support

**Success Criteria:**
- Search finds events quickly
- Filters work correctly
- Export produces valid files
- Help is comprehensive
- Mouse clicks work

---

### Phase 6: Testing & Documentation (Week 11-12)

**Goals:**
- Comprehensive testing
- User documentation
- Deployment

**Deliverables:**
1. Full test coverage (>80%)
2. Integration tests
3. Performance benchmarks
4. User guide
5. Developer documentation
6. Build/release scripts

**Success Criteria:**
- All tests pass
- No known bugs
- Documentation complete
- Ready for student use

---

## Open Questions

### Technical Decisions

**Q1: Event Transport?**
- **Option A:** HTTP SSE (Server-Sent Events) - Simple, one-way
- **Option B:** WebSocket - Bidirectional, more complex
- **Option C:** File watching - Simplest, no server changes needed
- **Recommendation:** Start with HTTP polling/SSE, add WebSocket later

**Q2: Subagent Event Tracking?**
- Currently we only see SubagentStop
- Do we need to track subagent tool use separately?
- **Recommendation:** V1: Just show subagent lifecycle; V2: Full tracking

**Q3: Multi-Session Support?**
- Should UI show multiple sessions simultaneously?
- **Recommendation:** V1: One session at a time; V2: Session switcher

**Q4: Persistence?**
- Should sessions be saved to disk?
- Resume from previous session?
- **Recommendation:** V1: In-memory only; V2: Optional persistence

**Q5: Remote Monitoring?**
- Monitor Claude Code running on different machine?
- **Recommendation:** V1: Localhost only; V2: Remote via network

### UX Decisions

**Q6: Default Collapsed or Expanded?**
- New requests arrive collapsed or expanded?
- **Recommendation:** Collapsed by default, auto-expand if error

**Q7: Auto-Scroll?**
- Follow newest event automatically?
- **Recommendation:** Yes, with toggle to freeze

**Q8: Color Scheme?**
- Dark mode only or support light mode?
- **Recommendation:** Dark mode V1, theme support V2

**Q9: Export Format?**
- JSON, CSV, or custom format?
- **Recommendation:** JSON for full data, markdown for reports

**Q10: Notification?**
- Desktop notifications on errors?
- **Recommendation:** V2 feature, optional

---

## Appendix: Code Organization

### Directory Structure

```
claude-hooks-monitor-ui/
├── cmd/
│   └── monitor-ui/
│       └── main.go                 # Entry point
│
├── internal/
│   ├── model/                      # Data model
│   │   ├── session.go
│   │   ├── request.go
│   │   ├── event.go
│   │   └── statistics.go
│   │
│   ├── processor/                  # Event processing
│   │   ├── processor.go
│   │   ├── grouper.go
│   │   └── linker.go
│   │
│   ├── receiver/                   # Event ingestion
│   │   ├── sse.go
│   │   ├── websocket.go
│   │   └── file.go
│   │
│   ├── ui/                         # UI layer
│   │   ├── app.go                  # Bubble Tea app
│   │   ├── model.go                # Main model
│   │   ├── update.go               # Update logic
│   │   ├── view.go                 # View rendering
│   │   │
│   │   ├── components/             # Reusable components
│   │   │   ├── tree.go
│   │   │   ├── detail.go
│   │   │   ├── stats.go
│   │   │   └── help.go
│   │   │
│   │   ├── views/                  # View modes
│   │   │   ├── tree_view.go
│   │   │   ├── detail_view.go
│   │   │   ├── json_view.go
│   │   │   └── stats_view.go
│   │   │
│   │   └── styles/                 # Lipgloss styles
│   │       └── styles.go
│   │
│   ├── state/                      # State management
│   │   ├── state.go
│   │   └── persistence.go
│   │
│   └── util/                       # Utilities
│       ├── format.go               # Formatting helpers
│       ├── tree.go                 # Tree algorithms
│       └── clipboard.go            # Clipboard ops
│
├── pkg/                            # Public API (if needed)
│   └── client/
│       └── client.go
│
├── test/
│   ├── fixtures/                   # Test data
│   ├── integration/                # Integration tests
│   └── visual/                     # Visual regression tests
│
├── docs/
│   ├── ARCHITECTURE.md
│   ├── USER_GUIDE.md
│   └── DEVELOPER.md
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### Module Dependencies

```go
// go.mod
module github.com/your-org/claude-hooks-monitor-ui

go 1.21

require (
    github.com/charmbracelet/bubbletea v0.25.0
    github.com/charmbracelet/bubbles v0.18.0
    github.com/charmbracelet/lipgloss v0.9.1
    github.com/atotto/clipboard v0.1.4
    github.com/stretchr/testify v1.8.4
)
```

---

## Conclusion

This design provides a comprehensive blueprint for building an interactive, hierarchical, real-time terminal UI for monitoring Claude Code hooks. The architecture is:

- **Modular:** Clear separation of concerns
- **Performant:** Optimized for live streaming and large datasets
- **Testable:** Designed with testing in mind
- **Educational:** Focused on student learning and debugging
- **Extensible:** Easy to add features incrementally

**Next Steps:**
1. Review and approve this design
2. Set up project structure
3. Begin Phase 1 implementation
4. Iterate based on feedback

**Estimated Timeline:** 12 weeks to full feature set

---

**End of Design Document**
