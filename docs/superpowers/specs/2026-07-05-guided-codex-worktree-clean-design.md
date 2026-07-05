# Guided Codex Worktree Clean Design

Date: 2026-07-05
Status: Approved design

## Context

`aibris clean` is safe by default: active worktrees are protected, risky
categories require opt-in, and real deletion still asks for confirmation unless
forced. That safety is correct, but heavy Codex usage creates a practical gap.
`~/.codex/worktrees` can grow by tens of gigabytes before the default `7d` age
policy feels useful, and many large entries remain `active` forever because
their parent git metadata still exists.

Local dogfood on this machine showed the shape of the problem:

- `~/.codex` contained 40 worktree items totaling about 43.5 GB.
- Those worktrees were all active, so default clean protected the entire
  worktree total.
- `node_modules` cleanup could recover about 19.6 GB at `7d`, but that does
  not address the largest worktree storage pressure.
- Some large active worktrees had no matching Codex session metadata, while
  others had recent Codex sessions and should remain protected.

The next UX should help users make a high-confidence selection among active
Codex worktrees. It should not silently delete active worktrees.

## Goals

- Add a guided cleanup path for Codex worktrees that defaults low-risk items to
  selected and lets the user deselect items before previewing.
- Use Codex session metadata as an advisory signal: session count, latest
  session time, and whether the same project has newer Codex activity.
- Combine session signals with local git safety checks before selecting active
  worktrees by default.
- Preserve the existing cleanup safety contract: preview first, then explicit
  confirmation, then deletion.
- Avoid reading Codex conversation bodies. Only `session_meta` metadata should
  be used.
- Keep `scan --json` stable and keep the recommendation UX in `cmd`
  presentation/guidance code.

## Non-Goals

- Automatically delete active worktrees without user interaction.
- Build a full-screen terminal UI in this pass.
- Infer semantic task completion from conversation content.
- Modify Codex session files.
- Add a general scoring engine for every debris category.
- Replace the existing `--interactive` per-item confirmation flow.
- Change the default behavior of plain `aibris clean`.

## User-Facing Command

Add a guided command mode:

```bash
aibris clean --guide --category worktree --tool codex
```

When `--guide` is used without an explicit category/tool, it should imply
`--category worktree --tool codex`. Broader guided modes can be added later,
but the current product problem is active Codex worktree bloat.

The guide should present a preselected review list:

```text
guided codex worktree cleanup

scan
  source     cached, 34s old
  activity   indexed, 12s old

summary
  worktrees          40 items   43.5 GB
  default selected   12 items   18.4 GB
  protected          28 items   25.1 GB

selected for cleanup
  [x]  1   4.6 GB  baby_ops-dogfood-install      active 9d   sessions 0   clean git
  [x]  2   1.6 GB  beopjalal-actions-runner...   active 11d  sessions 0   clean git
  [x]  3   1.1 GB  baby_ops-correction-a11y      active 11d  newer baby_ops activity

protected
  [ ]  4   5.2 GB  d9f73.../tamgu_note           latest session 2026-07-03
  [ ]  5   1.1 GB  3247/baby_ops                 latest session today
  [ ]  6   1.0 GB  some-worktree                 dirty files

Enter numbers to toggle, Enter to preview, q to abort:
```

After the user accepts the selection, the command should render the normal
dry-run clean plan for the selected items. Real deletion still requires a final
confirmation unless `--force` is explicitly provided.

## Default Selection Policy

The guide may default-select an active Codex worktree only when all required
safety checks pass and at least one low-risk signal is present.

Required checks:

- The item is under `~/.codex/worktrees`.
- The item is not the current process working directory and does not contain
  the current process working directory.
- The worktree path exists and is a discovered worktree entry path, not a
  nested duplicate target.
- The local git worktree is clean: no staged, unstaged, or untracked files.
- Upstream comparison is safe. If upstream is configured, the worktree must not
  have unpushed commits. If upstream is missing or comparison fails, protect
  the item by default.
- The item is at least `1d` old unless the user explicitly lowers the guide
  age.
- The item is not the newest known worktree for its project.
- The item is at least 256 MB. Smaller clean worktrees can still be shown, but
  they should not be default-selected because the deletion value is low.

Low-risk signals:

- The worktree has zero matching Codex sessions.
- The worktree has no recent matching Codex session and the same project has
  newer Codex activity elsewhere.
- The worktree is large enough to matter, with size used for ranking but not as
  a sole deletion signal.

Protection reasons should be explicit:

- `dirty files`
- `unpushed commits`
- `current working directory`
- `newest project worktree`
- `latest session today`
- `recent Codex activity`
- `git status unavailable`
- `upstream comparison unavailable`

## Codex Activity Index

Codex activity should be read from session metadata only. The relevant fields
are expected in the first `session_meta` line of session jsonl files:

```text
payload.cwd
timestamp
payload.session_id or payload.id
```

The guide should build or reuse an activity index with one row per worktree ID:

```go
type codexWorktreeActivity struct {
	WorktreeID    string
	SessionCount  int
	LatestSession time.Time
}
```

The guide also needs project-level aggregates:

```go
type codexProjectActivity struct {
	Project       string
	SessionCount  int
	LatestSession time.Time
}
```

The index should live under the user cache directory, for example:

```text
~/Library/Caches/aibris/codex-activity.json
```

The cache should be treated as advisory. If it is missing or stale, the command
may rebuild it by scanning session file metadata. If rebuild fails, the guide
must still work, but it should protect active worktrees by default and explain
that Codex activity was unavailable.

## Performance

Scanning all Codex session jsonl files on every guided cleanup is too expensive
for an interactive cleanup path. The activity index should support incremental
or freshness-based reuse.

Acceptable first implementation:

- Store the indexed session file path, modtime, and size.
- Re-read only files whose metadata changed or are new.
- Drop records for removed session files.
- Keep a simple schema version so the index can be rebuilt after format
  changes.

The guide should print whether it used a cached or rebuilt activity index.
The default freshness window for the activity index is 15 minutes. If the index
is older, the guide should incrementally refresh it before building
recommendations.

## Git Safety Checks

For each candidate worktree, inspect git state from the worktree directory:

```bash
git -C <path> status --porcelain=v1 --branch
git -C <path> rev-parse --abbrev-ref --symbolic-full-name @{u}
git -C <path> rev-list --count @{u}..HEAD
```

Equivalent Go implementation is fine. The behavior contract matters:

- Any dirty or untracked output protects the item.
- Any unpushed commit protects the item.
- Any git command failure protects the item.
- Detached or no-upstream states protect the item unless a later design defines
  a stronger rule.

The guide should not run expensive project-specific commands such as tests,
package managers, or linters.

## Duplicate And Overlap Handling

Before guided cleanup is implemented, target identity must be path-safe:

- A worktree entry path must appear at most once in a cleanup plan.
- If a selected worktree path contains selected `node_modules` targets, the
  nested targets must not be counted or removed separately.
- If one Codex worktree entry contains multiple nested git worktrees, the guide
  must either present the entry once with multiple project names or protect it
  until the scanner can represent it without duplicate deletion targets.

This is required because deleting the same path twice produces misleading freed
space totals and confusing dry-run output.

## Architecture

Keep policy boundaries narrow:

- `internal/scanner`: discovers debris and sizes paths.
- `internal/cleaner`: filters and executes explicit cleanup targets.
- `cmd`: owns guided presentation, recommendation building, toggling, and
  conversion from selected recommendations to cleanup targets.

Suggested new command-layer flow:

```text
scanForClean(...)
  |
  v
dedupeCleanupTargets(...)
  |
  v
loadCodexActivityIndex(...)
  |
  v
inspectWorktreeGitState(...)
  |
  v
buildGuidedWorktreePlan(...)
  |
  v
render toggle prompt -> selected targets
  |
  v
printCleanPlan(selected, dry-run)
  |
  v
final confirmation -> cleaner.Execute(selected)
```

The guide can reuse existing clean audit sections where useful, but the
recommendation plan is a human-only surface. It should not be added to
`types.ScanResult`.

To keep output readable, the guide should show all selected rows and the top 20
protected rows by size. If more protected rows exist, print a compact remainder
line with count and size.

## Testing

Add focused tests for the recommendation contract:

- A clean active Codex worktree with zero matching sessions is selected by
  default.
- A dirty active worktree is protected.
- A worktree with unpushed commits is protected.
- The current working directory worktree is protected.
- The newest worktree for a project is protected.
- A stale worktree with newer same-project activity is selected when git checks
  are clean.
- Missing or invalid Codex activity index protects active worktrees by default.
- Duplicate worktree paths are shown and counted once.
- Nested `node_modules` targets are not double-counted when their parent
  worktree is selected.
- Toggling selected items updates the preview target list.
- Plain `aibris clean` behavior is unchanged.

Run the standard verification set after implementation:

```bash
go test ./...
go build ./...
go vet ./...
```

## Documentation

Update narrowly after implementation:

- `README.md`: document `clean --guide` and its safety model.
- `docs/SPEC.md`: add the guided Codex worktree cleanup contract.
- `docs/DOGFOOD.md`: add a transcript from a real local guided dry-run.
- `skills/aibris/SKILL.md`: teach the AI-guided workflow to prefer the guide
  for active Codex worktree bloat, while still requiring dry-run before real
  deletion.

## Fixed Defaults

- `--guide` without filters means `--category worktree --tool codex`.
- Codex activity index freshness is 15 minutes.
- Default selection requires at least 256 MB of reclaimable worktree size.
- Protected rows are truncated after the top 20 by size, with a remainder
  summary.
