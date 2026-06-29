# Scan and Cleanup Improvement Plan

## Problem

`aibris` is useful when it acts as an AI-development debris auditor, but the
current scan and clean behavior has three sharp gaps:

- `node_modules` scanning only covers `~/projects`, which misses common roots
  like `~/workspace`, `~/Developer`, `~/src`, and project-local agent worktrees.
- `clean` can target active worktrees based only on age/category/tool. The
  scanner records worktree health, but cleanup does not use it.
- Build and package caches are deleted by path. For caches with official
  maintenance commands, direct directory deletion is less trustworthy than
  using the owning tool.

## Goals

1. Expand scan coverage so users get a truthful inventory from `$HOME`.
2. Make worktree cleanup safer by default, with explicit behavior for active
   versus orphaned worktrees.
3. Prefer official cache cleanup commands where the behavior is explicit and
   testable.
4. Keep the CLI dumb and predictable. AI-assisted judgment stays outside the
   CLI through `scan --json`.

## Non-Goals

- General-purpose disk cleaner behavior outside `$HOME`.
- GUI, daemon, scheduled cleanup, or background agents.
- Cross-machine policy sync.
- Full branch/PR merge analysis in this pass.
- Removing arbitrary user-provided paths.

## What Already Exists

- `internal/adapter.NodeModulesAdapter` walks one project root and reports
  `node_modules` size and age.
- `internal/adapter.WorktreeAdapter` detects Codex, Claude, and generic
  `worktree*` paths and records `active`, `orphaned`, or `plain-dir`.
- `internal/cleaner.Filter` centralizes age/category/tool/risky filtering.
- `internal/cleaner.Execute` centralizes path safety and deletion.
- `cmd/clean.go` already has `--dry-run`, `--interactive`, and final
  confirmation.
- `scan --json` already carries structured items for AI-guided workflows.

## Shipped Implementation

### 1. Configurable Home-Scoped Scan Roots

Add explicit scan roots for project-style debris:

```text
default roots:
  $HOME

skip directories:
  .Trash
  Library
  Applications
  Pictures
  Movies
  Music
  node_modules
  .git
  vendor
```

Use `$HOME` as the default root for `node_modules` discovery, but prune known
high-noise personal/system directories. This keeps the promise the user expects:
"scan my home for development debris", not "scan one hardcoded folder".

Add optional `--root` to `scan` and `clean`:

```bash
aibris scan --root ~/workspace --root ~/Developer
aibris clean --root ~ --category node_modules --dry-run
```

`--root` accepts only absolute paths under `$HOME` after symlink resolution.
No `--root /`.

Normalize roots before scanning:

- expand `~`
- resolve symlinks when possible
- reject roots outside resolved `$HOME`
- sort and deduplicate roots
- drop nested roots when an ancestor root is already present

Represent this as an explicit scanner contract:

```go
type ScanOptions struct {
	Roots []string
}

type DebrisProvider interface {
	Name() types.Tool
	Category() types.Category
	Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error)
}
```

Keep `scanner.Scan(ctx)` as a compatibility wrapper that calls the default
scanner with `ScanOptions{Roots: []string{home}}`.

### 2. Worktree Cleanup Status Policy

Use existing `DebrisInfo.Status` in cleanup filtering.

Default behavior:

```text
worktree cleanup:
  orphaned  -> eligible when age matches
  active    -> excluded by default
  plain-dir -> ignored by scanner
```

Add `--include-active-worktrees` for users who intentionally want the current
age-based behavior. Active worktree deletion still requires normal confirmation.

This keeps the dangerous case explicit: deleting a valid worktree that may hold
uncommitted or unpushed work.

### 3. JSON Schema Additions

Add fields without removing existing fields:

```json
{
  "status": "orphaned",
  "risk": "low|medium|high",
  "reason": "orphaned worktree; parent repo metadata missing"
}
```

Treat `risk` and `reason` as JSON presentation fields derived from existing
`category` and `status` values. Do not add stored risk state to `DebrisInfo`
unless later cleanup logic needs it.

Keep the historical top-level `worktrees` field for compatibility.

### 4. Documentation Updates

Update:

- `README.md`
- `docs/SPEC.md`
- `docs/JSON_SCHEMA.md`
- `docs/CATEGORY.md`
- `skills/aibris/SKILL.md`

The docs must explain that `$HOME` scanning is bounded by pruning rules, and
that active worktrees are excluded by default.

### 5. Command-Backed Cache Cleanup

Supported caches use owning-tool commands when available:

- `go clean -cache`
- `npm cache clean --force`
- `uv cache prune`

Commands run through argv-only execution with context cancellation. Missing
commands fall back to safe path removal; commands that run and fail do not fall
back silently.

### 6. Scan and Clean UX Follow-Up

PR #30 added bounded-parallel provider scans, interactive terminal spinner
progress, a stable `scanned ...` summary line, and target plans before `clean`
confirmation.

### 7. Dogfood Follow-Up: Make `clean` Feel As Good As `scan`

Real usage exposed that `clean` still feels worse than `scan`:

- `clean` silently performs a full scan before showing candidates.
- `clean --dry-run` and real `clean` use different output shapes.
- unknown project values render as `?`, which reads like missing safety
  information instead of "not applicable".
- deletion progress is printed only after each item finishes, so a large
  `node_modules` removal or cache command can still look stuck.

The implemented UX fix is intentionally small:

1. Reuse the existing scan progress printer in `clean`.
   - Print `clean` and `roots` before scanning.
   - Show the same spinner and final `scanned ...` line used by `scan`.
   - After filtering, print `matched N candidates`.
2. Use one clean-plan renderer for both `--dry-run` and confirmation.
   - Keep `cleaner.Filter` as the policy boundary.
   - Move human presentation to `cmd/`, not `internal/cleaner`.
   - Include headers so the columns are self-explanatory.
3. Replace `?` with an explicit display value.
   - For global caches, show `global`.
   - For path-derived items where no project is known, show `-`.
   - Keep JSON fields unchanged for compatibility.
4. Print per-item start progress before destructive work.
   - Example: `removing 1/28 expo (node_modules) ...`
   - Then print `removed:` or `cleaned:` when the item completes.
   - Preserve all existing safe-path checks and cleanup-command semantics.

The follow-up cache pass writes a 5-minute last-scan snapshot and reuses it only
when roots, cache schema, and freshness match:

```text
aibris scan
  |
  +-- writes last scan snapshot with roots, version, timestamp

aibris clean
  |
  +-- same roots, schema, and fresh snapshot? use it
  |
  +-- otherwise run scan with progress
  |
  +-- always re-check path safety and existence before deletion
```

### 8. Clean Audit Output Follow-Up

The clean audit output pass is implemented from
`docs/superpowers/specs/2026-06-29-clean-audit-output-design.md`.
It keeps scan, filter, and execute contracts stable while adding policy,
scan-source, category reason, target reason, and receipt output in `cmd`.

## Data Flow

```text
CLI flags
  |
  v
ScanOptions{Roots}
  |
  v
scanner.Scan(ctx, opts)
  |
  +--> NodeModulesAdapter.Scan(ctx, roots)
  |       |
  |       +--> Walk HOME, prune noisy dirs, report node_modules
  |
  +--> WorktreeAdapter.Scan(ctx, roots)
  |       |
  |       +--> Known + generic patterns, report status
  |
  +--> Cache adapters
          |
          +--> Report path size and cleanup command where supported

ScanResult
  |
  v
cleaner.Filter(opts)
  |
  +--> age/category/tool/risky filters
  +--> worktree status policy
  |
  v
cleaner.Execute
  |
  +--> existing remove-path cleanup
```

## Test Plan

### Coverage Diagram

```text
CODE PATH COVERAGE
==================
[+] cmd/scan.go
    |
    +-- parse repeated --root flags
    |   +-- valid roots under HOME
    |   +-- invalid absolute path outside HOME
    |   +-- symlink-resolved escape outside HOME
    |
    +-- scanner.Scan(ctx, opts)
        +-- default wrapper keeps existing behavior
        +-- explicit roots flow to every provider

[+] internal/adapter/node_modules.go
    |
    +-- walk configured roots
    |   +-- find node_modules outside ~/projects
    |   +-- skip nested node_modules traversal
    |   +-- prune Library/.Trash/.git/vendor/media dirs
    |   +-- keep Desktop and Downloads in default scan
    |
    +-- context cancellation exits walk

[+] internal/adapter/worktree.go
    |
    +-- scan known Codex/Claude patterns under roots
    +-- scan generic worktree* patterns under roots
    +-- preserve active/orphaned status detection

[+] internal/cleaner/cleaner.go
    |
    +-- Filter()
        +-- non-worktree behavior unchanged
        +-- orphaned worktree eligible when age matches
        +-- active worktree excluded by default
        +-- active worktree included with IncludeActiveWorktrees

[+] cmd/clean.go
    |
    +-- parse --include-active-worktrees
    +-- prints scan progress while finding clean candidates
    +-- reuses a fresh compatible last-scan cache
    +-- falls back to live scan when cache is stale or roots differ
    +-- drops stale cached targets whose paths no longer exist
    +-- dry-run reports filtered targets
    +-- confirmation uses the same target renderer as dry-run
    +-- unknown project display never renders as "?"
    +-- execute still uses IsSafePath
    +-- prints per-item start progress before slow deletions or commands

[+] cmd/scan_cache.go
    |
    +-- writes cache under the user cache directory
    +-- stores schema version, created_at, normalized roots, and ScanResult
    +-- rejects stale, schema-mismatched, and root-mismatched snapshots
```

### Unit Tests

- `NodeModulesAdapter`
  - scans `~/node_modules` under `$HOME` roots when appropriate
  - finds nested `node_modules` under `~/workspace`, `~/Developer`, and custom
    roots
  - prunes `Library`, `.Trash`, `.git`, nested `node_modules`, and hidden
    noise directories
  - rejects roots outside `$HOME`
  - deduplicates nested roots such as `$HOME` plus `$HOME/workspace`

- `WorktreeAdapter`
  - supports roots instead of only global `$HOME` patterns
  - reports existing active/orphaned status unchanged

- `cleaner.Filter`
  - excludes active worktrees by default
  - includes orphaned worktrees when age matches
  - includes active worktrees only when `IncludeActiveWorktrees` is true
  - keeps non-worktree filtering behavior unchanged

- `cleaner.Execute`
  - preserves unsafe path rejection
  - preserves existing path removal behavior for non-worktree debris

- JSON serialization
  - includes new fields
  - preserves old `worktrees` array shape

### CLI Tests

- `aibris scan --root <home-subdir> --json`
- `aibris clean --category worktree --dry-run` excludes active worktrees
- `aibris clean --include-active-worktrees --dry-run` includes active worktrees
- `aibris clean --dry-run` includes scan progress and a candidate summary
- `aibris clean --dry-run` and confirmation plans share the same target format
- non-project debris displays `global` or `-`, never `?`
- forced cleanup prints start progress before `removed:` or `cleaned:`
- `aibris scan --root <home-subdir>` writes a last-scan cache
- `aibris clean --root <same-home-subdir> --dry-run` uses a fresh cache without
  live scan progress
- stale or root-mismatched scan cache falls back to live scan
- invalid `--root /tmp` fails with a clear error

## Failure Modes

| Failure | Handling |
|---------|----------|
| `$HOME` scan is too slow | prune noisy roots, keep `--root` for narrowing |
| `clean` appears hung during its implicit scan | reuse scan progress printer in `clean` |
| a large deletion appears hung | print per-item start progress before delete/command work |
| stale cached scan points at removed paths | reject old cache and re-check target path existence before presentation/deletion |
| cached scan was for different roots | require exact normalized root match |
| cache schema changes | require exact schema version match |
| user has projects under `~/Library` | default misses them, user can pass `--root` |
| active worktree contains valuable work | excluded by default |
| symlink root escapes `$HOME` | reject after `EvalSymlinks` |

## Worktree Parallelization Strategy

Sequential implementation is preferred. The shared data model and CLI flags
touch the same core modules, so parallel worktrees would create avoidable merge
conflicts.

Suggested order:

1. Add scan roots and root validation.
2. Expand `NodeModulesAdapter` and tests.
3. Add worktree status filtering and tests.
4. Add derived status/risk/reason fields to JSON output.
5. Update JSON/docs/skill workflow.
6. Reuse scan progress and target rendering in `clean`.
7. Add last-scan cache reuse after the no-cache UX is polished.

## Simplify Constraints

- Do not introduce a global config file in this pass.
- Do not add a plugin architecture for cleanup commands.
- Do not change cache cleanup execution semantics in this pass.
- Prefer small structs over new interfaces unless tests prove the seam is
  needed.
- Keep CLI flag parsing in `cmd/`, scan logic in `adapter/`, filtering in
  `cleaner/`.
- Avoid a broad scanner rewrite. Add options to the existing scanner shape.
- Keep display-only labels out of JSON compatibility fields.
