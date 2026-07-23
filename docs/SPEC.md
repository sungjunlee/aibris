# aibris Engineering Spec

## Goal

`aibris` is a single-binary CLI for scanning and cleaning disk debris created by
AI-assisted development workflows: temporary worktrees, dependency folders,
build caches, package caches, and AI tool logs.

The product stance is conservative cleanup for development machines. The CLI
does four things: discover development debris, report structured data, preview
filtered targets, and delete only inside conservative safety boundaries. Human
or AI-guided judgment usually happens outside the CLI; the guided Codex cleanup
mode is a conservative review surface that still uses the normal preview and
confirmation path before deletion.

## Non-goals

- General Mac system maintenance, app uninstall, daemon scheduling, or GUI.
- Git worktree creation, branch deletion, or arbitrary repository management.
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
| `--age`, `-a` | `7d` | Classic cleanup includes only items older than this duration. In guided cleanup an explicit value changes only the minimum idle age. Supports `h`, `d`, `w`, `mo`, and `y`; Go duration units such as `m` and `s` are also accepted. Must be positive. |
| `--category`, `-c` | empty | Comma-separated category filter. Empty means all categories allowed by `--risky`. |
| `--tool`, `-t` | empty | Comma-separated tool filter. Empty means all tools. |
| `--root` | `$HOME` | Repeatable scan root. Each root must resolve under `$HOME`. |
| `--dry-run` | `false` | Preview targets without deleting. |
| `--interactive`, `-i` | `false` | Confirm each item before deleting. |
| `--risky` | `false` | Include risky categories such as AI logs. |
| `--include-active-worktrees` | `false` | Include active Git worktrees in cleanup candidates. |
| `--force`, `-f` | `false` | Skip the final confirmation prompt. It does not bypass hard locks or force Git worktree removal. |
| `--guide` | `false` | Force the guided Codex worktree cleanup flow. When category/tool filters are omitted, it implies `--category worktree --tool codex`. When age is omitted, guided cleanup uses a 3-day minimum idle age; explicit `--age` changes only that value. |
| `--no-guide` | `false` | Keep the classic cleanup audit/executor route even when active Codex pressure would open guided review. |

Behavior:

1. Parse and validate `--age`.
2. Warn when `--age` is shorter than one hour.
3. Obtain scan results from a fresh compatible scan cache or by scanning providers.
4. Choose guided or classic cleanup. Classic cleanup filters by age, category,
   tool, risky status, and worktree health. Guided cleanup builds physical
   cleanup units, collects Git and activity evidence, and applies the
   hierarchical policy below.
5. Print a cleanup audit with policy, scan source, eligible totals, and skipped reasons.
6. If no targets match, print `No items to clean.` and exit 0.
7. If `--dry-run` is set, print targets and total candidate space without deleting.
8. If `--interactive` is set, ask per item.
9. If not forced, print the target plan and ask for one final confirmation.
10. Delete ordinary and orphaned targets through cleaner safety checks. Delete
    active worktree members through the Git-aware executor.
11. Print a cleanup receipt with removed, partial, and failed unit counts,
    truthful freed bytes, and protected/skipped totals.

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
- cleanup receipt after execution with removed, partial, and failed unit counts,
  truthful freed bytes, and protected/skipped totals

The audit is human output only. `scan --json` remains the machine-readable surface for agents and scripts.

Default guided Codex worktree cleanup:

- Plain `clean` and `clean --dry-run` build a guided plan when no classic
  selector is supplied, at least one validated active Codex cleanup unit exists,
  and pressure is at least 256 MB or three units. Routing does not require an
  initially recommended row, so protected-only pressure is still reviewable.
- Classic selectors such as `--category`, `--tool`, `--risky`,
  `--include-active-worktrees`, `--interactive`, and `--force` keep the classic
  cleanup route unless `--guide` is explicit.
- `--root` only narrows scan scope; it does not disable default guided routing.
- `--no-guide` disables the default guided route and keeps the classic audit.
- `--guide` explicitly forces guided Codex worktree cleanup.
- `recommended` rows start selected. `reviewable` rows are soft-policy holds and
  may be toggled. `locked` rows remain visible and cannot be selected.
- The planner must fail closed when Codex activity or git safety evidence is
  unavailable or unsafe.
- Codex activity uses metadata only: session metadata, working-directory paths,
  timestamps, and cache file metadata. It must not read conversation bodies.
- Git safety protects current working directories, dirty or untracked members,
  unreadable evidence, and detached HEADs not reachable from named refs.
  Missing or gone upstream is explanatory metadata, not a lock. An attached
  local branch remains recoverable without an upstream.
- Pressing Enter renders the existing dry-run target plan for selected rows.
- Real deletion still requires confirmation unless `--force` is explicitly set;
  `--dry-run` never deletes.

Guided cleanup unit and policy contract:

- One canonical target path is one physical cleanup unit, counted and removed
  once. Every direct or one-level nested `.git` member is inspected; any hard
  failure locks the entire unit.
- Canonical, symlink-resolved Git common-dir is repository identity. Display
  project names do not control retention, and a multi-repository unit is
  retained when any member repository retains it.
- Member activity is the maximum trusted timestamp from matching Codex session
  metadata, per-worktree HEAD reflog, and scanner metadata fallback. Codex
  activity availability remains a separate required signal.
- Policy order is hard lock, recent-three repository retention, minimum idle
  age, minimum size, then recommendation.
- Hard locks are: current working directory containment; dirty or untracked
  members; unavailable Git or Codex activity evidence; detached HEAD not
  reachable from a named local or remote ref; and activity within 6 hours.
- The three most recent units per canonical repository are reviewable. Ranking
  includes locked units and is deterministic by activity then stable unit key.
- The guided minimum idle age defaults to 3 days and the recommendation size to
  256 MB. Younger or smaller safe units are reviewable.
- Explicit `--age` and prompt age controls change only minimum idle age. They do
  not alter the 6-hour lock or recent-three ranking. User selection overrides
  survive replanning while a row remains selectable.

Git-aware active execution contract:

- Capture repository identity, HEAD, refs, and member set before confirmation;
  rebuild and compare them immediately before mutation.
- Preflight every member before removing any member. A changed HEAD, new or
  missing member, dirty state, CWD containment, or lost recoverability aborts
  the whole unit before mutation.
- Remove each active member with `git --git-dir=<common-dir> worktree remove
  <path>`, without `--force`. Never fall back to raw recursive deletion when
  this command fails.
- After removal, verify the member path and parent worktree metadata are gone,
  attached branch refs still resolve to the captured OID, and a captured named
  ref still reaches a detached HEAD.
- For a partial multi-member failure, stop, preserve the remaining container,
  identify each removed and remaining member, and credit no bytes unless the
  physical unit is gone.

### FR4 - AI-guided Skill Workflow

`skills/aibris/SKILL.md` defines the intended AI-assisted cleanup flow:

1. Run `aibris scan --json`.
2. Summarize by project, category, size, and age.
3. For an unscoped active Codex worktree cleanup, use the separate no-selector
   guided branch: preview with `aibris clean --dry-run`, ask again, and execute
   with `aibris clean` only after the user approves the guided selection.
4. For ordinary cleanup groups, ask the user which groups to remove.
5. For a scoped cleanup, build the command from every approved `--category`,
   `--tool`, repeatable `--root`, and `--age` value and every applicable routing
   or safety flag.
6. Run that exact scoped command with `--dry-run` appended.
7. Ask for confirmation again.
8. Run the exact same scoped command after explicit approval, removing only
   `--dry-run`. Never replace it with plain `aibris clean`.

## Supported Categories

| Category | Default clean | Tools | Default locations |
|----------|---------------|-------|-------------------|
| `worktree` | orphaned only | `codex`, `claude`, `unknown` | Bounded shallow discovery of `$HOME` directories named `worktrees`, `worktree`, `worktree-*`, or `worktrees-*`, validated by direct or nested `.git` files |
| `node_modules` | yes | `node_modules` | `$HOME/**/node_modules`, with noisy system/media/cache directories pruned |
| `build-cache` | yes | `build-cache` | `~/.cache/go-build`, `~/.gradle/caches`, `~/.npm/_cacache`, `~/.cargo/registry`, `~/Library/Caches/Xcode` |
| `other-cache` | yes | `pip-cache` | `~/.cache/pip`, `~/.cache/uv` |
| `ai-logs` | no, requires `--risky` | `ai-logs`, `cursor`, `windsurf` | known Codex, Claude, Cursor, and Windsurf log/cache locations |

## Worktree Health

`WorktreeAdapter` detects linked Git metadata health by reading `.git` files:

| Status | Meaning |
|--------|---------|
| `active` | `.git` exists and parent repository metadata still exists. This means linked, not recently used. |
| `orphaned` | `.git` exists but parent repository metadata is gone. |
| `plain-dir` | No valid worktree metadata was found. |

Cleanup excludes `active` worktrees by default. `orphaned` worktrees remain
eligible when age/category/tool filters match. Use `--include-active-worktrees`
to intentionally include active worktrees in classic cleanup. The default
guided Codex route may recommend linked active units only after the cleanup-unit
policy passes.

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
- Classic cleanup must exclude active worktrees unless
  `--include-active-worktrees` is set. Guided Codex recommendations instead
  require cleanup-unit hard safety.
- Active worktree execution must preserve branch refs, revalidate the full
  member set, use non-forced Git worktree removal, and fail closed without raw
  path fallback.
- `--force` must only skip final confirmation; it must not unlock rows or become
  Git's force option.
- Command-backed cleanup must use argv-only execution and context cancellation.
- `--dry-run` must never delete.
- `clean` must ask for confirmation unless `--force` or `--interactive` is set.
- Context cancellation must be checked during scans and directory walks.
- Adapter failures must not silently abort unrelated providers. A usable
  partial scan must identify failed providers in human and JSON output, emit
  the retained result, and exit with status 1.
- Partial scans must not be cached or accepted as cleanup prerequisites.
  Cancellation remains a hard failure with no usable partial result.

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
