# aibris — Category System Design

## Overview

`aibris clean` currently handles one cleanup target: AI tool git worktrees (codex, claude).
The category system generalizes cleanup to arbitrary disk debris categories (node_modules,
build caches, pip caches, etc.) while keeping the CLI simple and agent-friendly.

```
aibris scan                         → all categories
aibris scan --json                  → machine-readable (agent consumption)
aibris clean                        → all categories, non-interactive
aibris clean --category node_modules → single category
aibris clean --tool codex --age 24h → AND filters
```

## Category Type

```go
// internal/types/types.go
type Category string

const (
    CategoryWorktree    Category = "worktree"
    CategoryNodeModules Category = "node_modules"
    CategoryBuildCache  Category = "build-cache"
    CategoryOtherCache  Category = "other-cache"
)
```

## Interface Extension

Each adapter declares its category. One adapter = one category.

```go
// internal/adapter/adapter.go
type WorktreeProvider interface {
    Name() types.Tool
    Category() types.Category   // NEW
    Scan(ctx context.Context) ([]types.WorktreeInfo, error)
}
```

## Data Model

```go
type WorktreeInfo struct {
    Tool     Tool
    Category Category   // NEW
    ID       string
    Project  string
    Path     string
    Size     int64
    ModTime  time.Time
}

type ScanResult struct {
    Worktrees  []WorktreeInfo
    TotalCount int
    TotalSize  int64
    ByCategory map[Category]CategorySummary   // NEW
    ByTool     map[Tool]ToolSummary           // NEW
}

type CategorySummary struct {
    Count int
    Size  int64
}

type ToolSummary struct {
    Count int
    Size  int64
}

type PruneOptions struct {
    Age         time.Duration
    Categories  []Category   // NEW: replaces All bool
    Tools       []Tool
    DryRun      bool
    Interactive bool
}
```

## JSON Output (`scan --json`)

```json
{
  "worktrees": [
    {
      "tool": "codex",
      "category": "worktree",
      "id": "abc123",
      "project": "my-project",
      "path": "/Users/sj/.codex/worktrees/abc123",
      "size": 1048576,
      "mod_time": "2026-05-19T10:30:00Z"
    }
  ],
  "summary": {
    "total_count": 5,
    "total_size": 5242880,
    "by_category": {
      "worktree": {"count": 3, "size": 3145728},
      "node_modules": {"count": 2, "size": 2097152}
    },
    "by_tool": {
      "codex": {"count": 3, "size": 3145728},
      "claude": {"count": 2, "size": 1048576},
      "node_modules": {"count": 2, "size": 2097152}
    }
  }
}
```

## Filter Logic (cleaner.go)

```go
func Filter(worktrees []WorktreeInfo, opts PruneOptions) []WorktreeInfo {
    cutoff := time.Now().Add(-opts.Age)
    for _, w := range worktrees {
        matchCat := len(opts.Categories) == 0 || containsCategory(opts.Categories, w.Category)
        matchTool := len(opts.Tools) == 0 || containsTool(opts.Tools, w.Tool)
        if matchCat && matchTool && w.ModTime.Before(cutoff) {
            filtered = append(filtered, w)
        }
    }
    return filtered
}
```

No `--all` flag needed. Both `--category` and `--tool` empty = all.

## CLI Changes

```
REMOVED:
  --all                     → replaced by len(Categories)==0

MODIFIED:
  --tool                    → AND filter with --category

NEW:
  --category <name>         → repeatable? or comma-separated?
  --json                    → JSON output (scan only)
```

## Phase 2 Adapters

| Adapter | Category | Default Paths | Risk Level |
|---------|----------|---------------|------------|
| `CodexAdapter` | `worktree` | `~/.codex/worktrees/*` | Low |
| `ClaudeAdapter` | `worktree` | `~/*/.claude/worktrees/*` | Low |
| `NodeModulesAdapter` | `node_modules` | `~/projects/**/node_modules/` | Medium |
| `BuildCacheAdapter` | `build-cache` | `~/Library/Caches/Xcode/`, `~/.cache/go-build/` | Medium |
| `PipCacheAdapter` | `other-cache` | `~/.cache/pip/`, `~/.cache/uv/` | Low |

## Agent Integration Pattern

```
Agent flow:
  1. agent calls:  aibris scan --json
  2. agent parses JSON, summarizes for user
  3. user picks categories/tools/age
  4. agent calls:  aibris clean --category node_modules --dry-run
  5. user confirms
  6. agent calls:  aibris clean --category node_modules

All decision-making stays in the agent.
aibris only does scan + clean; never asks questions in agent mode.
```

## Migration from v0.1.0

```
v0.1.0                              v0.2.0
aibris scan            →  aibris scan                   (unchanged)
aibris prune --all     →  aibris clean                  (renamed)
aibris prune --force   →  aibris clean                  (prompt removed)
aibris prune --tool X  →  aibris clean --tool X         (unchanged)
                        →  aibris clean --category node_modules  (NEW)
                        →  aibris scan --json           (NEW)
```

## Implementation Order

```
Phase 2a (foundation) — no new adapters
─────────────────────────────────────
1. Category type + constants
2. Category() on WorktreeProvider interface
3. WorktreeInfo.Category field
4. Existing adapters set CategoryWorktree
5. ScanResult.ByCategory / ByTool
6. scan --json flag
7. PruneOptions.Categories (replace All)
8. Filter uses Categories
9. clean --category flag
10. Remove --all flag from CLI

Phase 2b (expansion) — new adapters
────────────────────────────────────
11. NodeModulesAdapter
12. BuildCacheAdapter
13. PipCacheAdapter
14. Register all in scanner.providers
```

## Out of Scope

- Auto-detect categories without explicit adapter registration
- Category-aware `--dry-run` grouping
- Per-category confirmation in interactive mode
