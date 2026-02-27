# Task: Generate CLAUDE.md Files for Every Directory

## Context

This project (hooks4claude) is a Go monorepo. Claude Code wastes ~53% of file reads re-reading files it has already seen, mostly during batch scans where it reads 5-16 files consecutively to rebuild its mental model of the project. These re-reads happen because context drifts after ~30 tool calls and because each new session starts with no memory of previous sessions.

The solution is to pre-populate every directory with a CLAUDE.md file containing AI-optimized package summaries. Claude Code auto-loads root CLAUDE.md at session start and loads child CLAUDE.md files on-demand when entering a directory. This gives Claude the architectural context it needs without reading every source file.

## What To Do

1. **Read the existing root CLAUDE.md** — do NOT replace it. Only append the sections described below.
2. **Walk every directory** in the project that contains files (source, config, docs, etc).
3. **Create a CLAUDE.md** in each directory that doesn't already have one.
4. **Follow the templates below** based on directory type.

## Root CLAUDE.md — Append Only

Add these sections to the END of the existing root CLAUDE.md. Do not modify or remove existing content.

### Section to add: "File Architecture Map"

A compact reference of what each package does, its key types, and its key functions. One or two lines per package. This section replaces the need for Claude to batch-scan the project at session start. Format:

```
## File Architecture Map

hooks-store/internal/store/ — {one-line purpose}. Key types: {types}. Key funcs: {funcs}.
hooks-store/internal/ingest/ — {one-line purpose}. Key types: {types}. Key funcs: {funcs}.
[... one line per Go package ...]
claude-hooks-monitor/internal/tui/ — {one-line purpose}. {count} components.
```

Keep it extremely compressed. This section should not exceed ~400 tokens total. Its purpose is to let Claude know what exists where so it can decide which packages to read in detail.

### Section to add: "Active Files"

```
## Active Files (read source before editing, don't rely on summaries)

- [list files currently being actively developed/modified]
```

If there are no currently active files, write: "No files currently under active development."

### Section to add: "CLAUDE.md Maintenance"

```
## CLAUDE.md Maintenance

Every directory has a CLAUDE.md with package summaries. Prefer summaries over re-reading stable source files for context. When you need to edit a file, always read the actual source. After changing a package's exported API, update its CLAUDE.md.
```

## Child CLAUDE.md — Three Templates

Determine the directory type and use the corresponding template.

### 📦 PACKAGE — Directory contains `.go` files

This is the most important type. Read every `.go` file in the directory and produce a summary.

**What to include:**
- Package name and one-sentence purpose
- Stability line (see below)
- For each `.go` file: exported types with field names and types (include struct tags), exported function signatures with parameter types, return types, and a brief description
- Concurrency primitives used (sync.Mutex, channels, goroutines, atomic, WaitGroup, etc)
- Which other internal packages this package imports

**What NOT to include:**
- Function bodies or implementation logic
- Unexported (private) functions, types, or variables
- Line numbers
- TODOs or temporary workarounds
- Comments that just restate the code

**Stability line:** If none of the files in the directory are listed in root CLAUDE.md's "Active Files" section, write: `All files stable — prefer this summary over reading source files.` Otherwise write: `Contains active files — read source before editing. Summary is accurate for context.`

**Format example (do not copy this content, generate from actual source):**

```markdown
# {package_name} — {one-line purpose}

{stability line}

## {filename}.go
{exported types and function signatures}

## {filename}_test.go
{test function names, one line each}
```

**Special case — struct definition files:** When a file's primary content is a struct definition (like a data model), include the full struct with field names, types, and tags. Struct definitions ARE their own summary — they don't compress further.

**Token budget:** Aim for 100-300 tokens per package CLAUDE.md. Larger packages (5+ files) can go up to 400 tokens. Never exceed 500 tokens.

### ⚙️ CONFIG — Directory has files but no `.go` source

For directories containing config files, documentation, module files (go.mod, Makefile, etc).

**Format:**

```markdown
# {directory_name}

{one-line purpose}

## Files
- {filename}: {one-line description of what it contains/configures}
```

**Token budget:** 50-150 tokens.

### 📂 STRUCTURAL — Directory contains only subdirectories

For directories that are just organizational containers (like `cmd/`, `internal/`).

**Format:**

```markdown
# {directory_name}

Subpackages:
- {subdir}/ — {one-line purpose}
```

**Token budget:** 30-80 tokens.

## Quality Guidelines

**Compression target for Go packages:** A good summary is 15-25% of the original source file size. If your summary is longer than 30% of the source, you're including too much implementation detail. If it's under 10%, you're probably missing exported types or signatures.

**Exception — struct-heavy files:** Files that are primarily struct definitions (data models, config types) compress poorly because the field list IS the summary. These will be 50-80% of source size. That's correct.

**Test files:** Only list test function names, one per line. Do not summarize test logic. If there are table-driven tests, note that.

**Cross-referencing:** When a type or function in one package directly depends on a type from another package in this monorepo, note it. Example: `Add(events []hookevt.HookEvent) error` — the `hookevt.HookEvent` reference tells Claude which package to look at for the type definition.

## Execution Order

1. Read the existing root CLAUDE.md
2. Read the full directory tree to identify all directories
3. Create CLAUDE.md files starting from the deepest (leaf) packages and working up to structural directories — this way, when writing structural indexes, you already know what each child package does
4. Append the three new sections to root CLAUDE.md last, since the "File Architecture Map" summarizes all the packages you just documented

## Verification

After creating all files, run:

```bash
find . -name "CLAUDE.md" -not -path "./.git/*" | wc -l
```

The count should equal the number of directories containing files or subdirectories, plus the root. Every directory that a developer or Claude might `cd` into should have a CLAUDE.md.

Then spot-check a few package CLAUDE.md files against their source:
- Does the summary include all exported types?
- Does it include all exported function signatures?
- Does it mention concurrency primitives if any exist?
- Is it under 500 tokens?
- Does the stability line match the root CLAUDE.md "Active Files" list?
