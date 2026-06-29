# aibris Engineering Spec

## Goal

`aibris` is a single-binary CLI for scanning and cleaning disk debris created by
AI-assisted development workflows: temporary worktrees, dependency folders,
build caches, package caches, and AI tool logs.

The product stance is conservative cleanup for development machines. The CLI
does four things: discover development debris, report structured data, preview
filtered targets, and delete only inside conservative safety boundaries. Human
or AI-guided judgment happens outside the CLI.

## Non-goals

- General Mac system maintenance, app uninstall, daemon scheduling, or GUI.
- Git worktree creation or repository management.
- Deleting arbitrary user-provided paths.
- Broad system cleanup outside development debris conventions.
- Automatic inference that recent or ambiguous files are safe to delete.

## Functional Requirements

### FR1 - `aibris scan`

- Run all registered `DebrisProvider` adapters.
- Run providers with bounded parallelism so slow discovery in one category does
  not block unrelated categories from starting.
- Continue scanning when a non-context adapter error occurs; write
  `scan:<tool>:<error>` to stderr.
- Return context cancellation and deadline errors immediately.
- Sort discovered items by size descending.
- Print progress for human-readable scans. Interactive terminals use a
  single-line spinner summary; non-interactive output uses plain progress
  lines suitable for logs.
- Print a human-readable summary, category breakdown, largest items, and next
  command suggestions after scanning.
- For empty scans, print an explicit zero-item summary and exit 0.
- Support `--json` for machine-readable agent workflows.
- Support repeated `--root` flags. Roots default to `$HOME`, may use `~`, must
  resolve under `$HOME`, and are sorted/deduplicated before scanning.

### FR2 - `aibris scan --json`

Output contains:

- `worktrees`: the compatibility field for all debris items. Entries may be
  non-worktree debris.
- `summary.total_count`
- `summary.total_size`
- `summary.by_category`
- `summary.by_tool`
- item-level `status`, `risk`, and `reason` fields for agent decisions
- item-level `source` for path-derived worktree owners
- item-level `cleanup_kind` and `cleanup_command` fields for cleanup execution

The schema is documented in `docs/JSON_SCHEMA.md`.

### FR3 - `aibris clean`

`clean` scans, filters, and then previews or deletes matching targets.

Flags:

| Flag | Default | Behavior |
|------|---------|----------|
| `--age`, `-a` | `7d` | Only include items older than this duration. Supports `h`, `d`, `w`, `mo`, and `y`; Go duration units such as `m` and `s` are also accepted. Must be positive. |
| `--category`, `-c` | empty | Comma-separated category filter. Empty means all categories allowed by `--risky`. |
| `--tool`, `-t` | empty | Comma-separated tool filter. Empty means all tools. |
| `--root` | `$HOME` | Repeatable scan root. Each root must resolve under `$HOME`. |
| `--dry-run` | `false` | Preview targets without deleting. |
| `--interactive`, `-i` | `false` | Confirm each item before deleting. |
| `--risky` | `false` | Include risky categories such as AI logs. |
| `--include-active-worktrees` | `false` | Include active Git worktrees in cleanup candidates. |
| `--force`, `-f` | `false` | Skip the final confirmation prompt. |

Behavior:

1. Parse and validate `--age`.
2. Warn when `--age` is shorter than one hour.
3. Scan all providers.
4. Filter by age, category, tool, risky status, and worktree health.
5. If no targets match, print `No items to clean.` and exit 0.
6. If `--dry-run` is set, print targets and total candidate space.
7. If `--interactive` is set, ask per item.
8. If not forced, print the target plan and ask for one final confirmation.
9. Delete targets through cleaner safety checks.
10. Print freed space.

Command-backed cleanup:

- `cleanup_kind=command` uses argv-only execution with `exec.CommandContext`.
- No shell string execution is allowed.
- Missing commands fall back to safe path removal for the scanned item.
- Commands that run and fail do not fall back silently.
- Context cancellation must stop command execution.

Human `clean` output must include a cleanup audit before deletion:

- roots and policy (`age`, `risky`, active worktree policy)
- scan source (`live` or `cached, <age> old`)
- scan summary with found, eligible, and protected/skipped totals
- category rows with found, eligible, protected/skipped, and main reason
- target plan with reason text before confirmation or dry-run completion
- cleanup receipt after execution that reports target count and freed bytes without claiming per-item success counts

The audit is human output only. `scan --json` remains the machine-readable surface for agents and scripts.

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
| `worktree` | orphaned only | `codex`, `claude`, `unknown` | Bounded shallow discovery of `$HOME` directories named `worktrees`, `worktree`, `worktree-*`, or `worktrees-*`, validated by direct or nested `.git` files |
| `node_modules` | yes | `node_modules` | `$HOME/**/node_modules`, with noisy system/media/cache directories pruned |
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

Cleanup excludes `active` worktrees by default. `orphaned` worktrees remain
eligible when age/category/tool filters match. Use `--include-active-worktrees`
to intentionally include active worktrees.

Worktree discovery is convention-based rather than a fixed tool list. Hidden
owner directories are intentionally allowed when they contain worktree roots,
for example `$HOME/.codex/worktrees`, `$HOME/.some-tool/worktrees`, or
`$HOME/project/.some-tool/worktrees`. The path-derived `source` field records
that owner as `.codex`, `.some-tool`, or `project-local` for plain
project-local `worktrees` directories.

Full-home discovery is bounded: aibris checks immediate hidden owners and
project-local containers within a shallow depth from each scan root. It does not
recursively traverse every descendant looking for arbitrarily deep worktree
owners.

## Scan Roots

Default scan roots are equivalent to resolved `$HOME`. `--root` narrows scan
scope and may be repeated:

```bash
aibris scan --root ~/.codex --root ~/path/to/project
```

Roots are expanded, symlink-resolved, rejected when they escape `$HOME`, sorted,
deduplicated, and collapsed when one root is nested inside another.

## Safety Requirements

- Destructive deletion must reject relative paths.
- Destructive deletion must reject paths outside `$HOME`.
- Destructive deletion must pass cleanup safety validation.
- Worktree deletion may also pass scanner-validated worktree safety: the target
  must resolve under `$HOME`, avoid symlink escape, and carry active/orphaned
  Git worktree metadata from scanning.
- `node_modules` discovered under valid home-scoped scan roots must remain
  eligible for cleanup safety checks.
- Risky categories must be excluded unless `--risky` is set.
- Active worktrees must be excluded unless `--include-active-worktrees` is set.
- Command-backed cleanup must use argv-only execution and context cancellation.
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
  scanner/    -> bounded-parallel provider orchestration and aggregation
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
	Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error)
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
