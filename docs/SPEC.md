# aibris Engineering Spec

## Goal

`aibris` is a single-binary CLI for scanning and cleaning disk debris created by
AI-assisted development workflows: temporary worktrees, dependency folders,
build caches, package caches, and AI tool logs.

The product stance is conservative cleanup for development machines. The CLI
does four things: discover known debris, report structured data, preview
filtered targets, and delete only inside known-safe boundaries. Human or
AI-guided judgment happens outside the CLI.

## Non-goals

- General Mac system maintenance, app uninstall, daemon scheduling, or GUI.
- Git worktree creation or repository management.
- Deleting arbitrary user-provided paths.
- Broad system cleanup outside known development and AI-tool locations.
- Automatic inference that recent or ambiguous files are safe to delete.

## Functional Requirements

### FR1 - `aibris scan`

- Run all registered `DebrisProvider` adapters.
- Continue scanning when a non-context adapter error occurs; write
  `scan:<tool>:<error>` to stderr.
- Return context cancellation and deadline errors immediately.
- Sort discovered items by size descending.
- Print human-readable output grouped by tool.
- Print `No AI tool debris found.` and exit 0 when no items are found.
- Support `--json` for machine-readable agent workflows.

### FR2 - `aibris scan --json`

Output contains:

- `worktrees`: the compatibility field for all debris items. Entries may be
  non-worktree debris.
- `summary.total_count`
- `summary.total_size`
- `summary.by_category`
- `summary.by_tool`

The schema is documented in `docs/JSON_SCHEMA.md`.

### FR3 - `aibris clean`

`clean` scans, filters, and then previews or deletes matching targets.

Flags:

| Flag | Default | Behavior |
|------|---------|----------|
| `--age`, `-a` | `7d` | Only include items older than this duration. Supports `h`, `d`, `w`, `mo`, and `y`; Go duration units such as `m` and `s` are also accepted. Must be positive. |
| `--category`, `-c` | empty | Comma-separated category filter. Empty means all categories allowed by `--risky`. |
| `--tool`, `-t` | empty | Comma-separated tool filter. Empty means all tools. |
| `--dry-run` | `false` | Preview targets without deleting. |
| `--interactive`, `-i` | `false` | Confirm each item before deleting. |
| `--risky` | `false` | Include risky categories such as AI logs. |
| `--force`, `-f` | `false` | Skip the final confirmation prompt. |

Behavior:

1. Parse and validate `--age`.
2. Warn when `--age` is shorter than one hour.
3. Scan all providers.
4. Filter by age, category, tool, and risky status.
5. If no targets match, print `No items to clean.` and exit 0.
6. If `--dry-run` is set, print targets and total reclaimable space.
7. If `--interactive` is set, ask per item.
8. If not forced, ask for one final confirmation.
9. Delete targets through cleaner safety checks.
10. Print freed space.

### FR4 - AI-guided Skill Workflow

`skills/aibris/SKILL.md` defines the intended AI-assisted cleanup flow:

1. Run `aibris scan --json`.
2. Summarize by project, category, size, and age.
3. Ask the user which groups to remove.
4. Run `aibris clean ... --dry-run`.
5. Ask for confirmation again.
6. Run `aibris clean ...` only after explicit approval.

## Supported Categories

| Category | Default clean | Tools | Default locations |
|----------|---------------|-------|-------------------|
| `worktree` | yes | `codex`, `claude`, `unknown` | `~/.codex/worktrees/*`, `~/*/.claude/worktrees/*`, `*/worktree*/*` |
| `node_modules` | yes | `node_modules` | `~/projects/**/node_modules` |
| `build-cache` | yes | `build-cache` | `~/.cache/go-build`, `~/.gradle/caches`, `~/.npm/_cacache`, `~/.cargo/registry`, `~/Library/Caches/Xcode` |
| `other-cache` | yes | `pip-cache` | `~/.cache/pip`, `~/.cache/uv` |
| `ai-logs` | no, requires `--risky` | `ai-logs`, `cursor`, `windsurf` | known Codex, Claude, Cursor, and Windsurf log/cache locations |

## Worktree Health

`WorktreeAdapter` detects Git worktree health by reading `.git` files:

| Status | Meaning |
|--------|---------|
| `active` | `.git` exists and parent repository metadata still exists. |
| `orphaned` | `.git` exists but parent repository metadata is gone. |
| `plain-dir` | No valid worktree metadata was found. |

Current cleanup filtering is based on age, category, tool, and risky status.
Worktree status is retained in the internal model for future reporting and
filtering.

## Safety Requirements

- Destructive deletion must reject relative paths.
- Destructive deletion must reject paths outside `$HOME`.
- Destructive deletion must pass `cleaner.IsSafePath`.
- Risky categories must be excluded unless `--risky` is set.
- `--dry-run` must never delete.
- `clean` must ask for confirmation unless `--force` or `--interactive` is set.
- Context cancellation must be checked during scans and directory walks.
- Adapter failures must not silently abort unrelated providers.

## Architecture

```
main.go       -> cmd.Execute()
cmd/          -> Cobra commands and CLI I/O
internal/
  adapter/    -> DebrisProvider implementations
  scanner/    -> provider orchestration and aggregation
  cleaner/    -> filtering, dry-run output, safe deletion
  types/      -> shared data model
skills/
  aibris/     -> AI-guided cleanup workflow
```

### Provider Contract

Each adapter implements:

```go
type DebrisProvider interface {
	Name() types.Tool
	Category() types.Category
	Scan(ctx context.Context) ([]types.DebrisInfo, error)
}
```

Adapter rules:

- Use kebab-case lowercase tool names.
- Respect `context.Context` cancellation.
- Use `estimateDirSize(ctx, path)` for reported size.
- Use `detectProjectName(path)` when project inference applies.
- Return `nil, nil` when default paths do not exist.
- Add focused tests for new discovery logic.

## Verification

Before release:

```bash
go test ./...
go build ./...
go vet ./...
goreleaser release --snapshot --clean
```

For release tags, GitHub Actions runs CI on push/PR and GoReleaser on `v*` tags.
