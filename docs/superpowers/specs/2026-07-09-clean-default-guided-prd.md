# Clean Should Default To Guided Decisions PRD

Date: 2026-07-09
Status: Proposed PRD

## Summary

`aibris v0.6.0` added the guided Codex worktree cleanup engine, but the product
entrypoint is still wrong. The best cleanup decision surface is hidden behind
`aibris clean --guide`, while the command users naturally run remains
`aibris clean` or `aibris clean --dry-run`.

The next product move is to make plain `clean` the smart cleanup entrypoint:
when no explicit filters are supplied and Codex worktree bloat is present,
`clean` should show the guided Codex review by default. `--guide` can remain as
an explicit force flag, but users should not need to know it exists.

This PRD updates the plan in two stages:

1. `v0.6.1`: auto-enter guided Codex cleanup from default `clean` when it is
   the highest-value decision surface.
2. `v0.7.0`: replace the number-entry guide with a real TTY checklist UI while
   keeping a text fallback for pipes, logs, and automation.

## Current State Review

The `v0.6.0` implementation is functionally correct:

- Installed CLI can be `aibris version 0.6.0`.
- `aibris clean --guide --dry-run --root ~/.codex` shows selected and protected
  Codex worktrees.
- The guide defaults low-risk rows to selected.
- Number toggles work.
- The selected rows hand off to the normal dry-run clean plan.
- `--dry-run` does not delete.

The product problem is discoverability and interaction quality:

- `aibris clean --dry-run` still follows the classic age/category cleanup
  audit path unless the user knows `--guide`.
- The default command is the most important CLI surface, and it is not the best
  experience for heavy Codex users.
- `[x]` and `[ ]` text rows plus number entry are not a true checklist
  experience. It is usable, but it feels like an internal tool.
- The docs and skill now mention `clean --guide`, but that still asks users and
  agents to remember a product-specific flag.

## Problem

Heavy Codex users accumulate active worktrees quickly. Age-only cleanup misses
the real pressure because active worktrees stay protected by default, and the
large safe-to-review candidates may be younger than the classic `7d` policy.

The command users reach for is:

```bash
aibris clean --dry-run
```

That command should answer the actual question:

> What can I safely reclaim right now, and why are the other large worktrees
> protected?

Today it answers a narrower implementation question:

> Which classic cleanup targets match the current filters?

The result is safe but underwhelming. Users do not see the guided Codex
decision surface unless they already know the hidden flag.

## Goals

- Make the default `clean` experience the most useful cleanup path.
- Automatically show guided Codex cleanup when no explicit filters are supplied
  and Codex worktree review is valuable.
- Preserve existing classic cleanup behavior for explicit filters and
  automation.
- Keep the dry-run-first and final-confirmation safety model unchanged.
- Add a clear escape hatch for classic audit output.
- Prepare the command-layer boundary for a later TTY checklist UI without
  mixing terminal rendering into scanner or cleaner internals.

## Non-Goals

- Do not silently delete active Codex worktrees.
- Do not make `scan --json` output carry recommendation policy.
- Do not build the full TTY checklist in the `v0.6.1` patch.
- Do not change behavior for explicit category/tool/risky cleanup commands.
- Do not require Codex conversation body reads. Activity remains metadata-only.
- Do not remove `--guide`; it remains useful for explicit testing and docs.

## Product Principles

### Default Command Must Be Pleasing

The CLI's default behavior is the product. Most users will not read the option
table. If `aibris clean --dry-run` can tell a better story, it should.

### Explicit Flags Mean Explicit Intent

When users pass `--category node_modules`, `--tool codex`, `--risky`, or
`--include-active-worktrees`, they are asking for the classic executor path.
Auto-guide should not override that.

### Guidance Is Not Permission

Guided selection is a recommendation surface. Deletion still requires a dry-run
plan and confirmation unless `--force` is explicitly supplied.

### Non-TTY Must Not Hang

Pipes, logs, and agents must not get stuck waiting for an interactive
full-screen UI. Non-TTY fallback should remain line-oriented and deterministic.

## Target User Jobs

### Human Developer

"My disk is filling up from Codex. Show me what I can probably remove without
making me understand the internal layout."

Expected command:

```bash
aibris clean --dry-run
```

### AI Assistant

"Analyze local cleanup candidates, show the user a safe recommended set, then
only delete after explicit approval."

Expected command:

```bash
aibris clean --dry-run
```

### Script Or Automation

"Show or execute classic filtered cleanup without surprise interaction."

Expected command:

```bash
aibris clean --category node_modules --age 7d --dry-run
```

## Recommended Plan

### Stage 1: v0.6.1 Auto-Guide Default

Ship a small patch release that changes entrypoint routing, not the core
planner.

Add a command-layer decision step after scan results are available and before
classic filtering:

```text
scanForClean(...)
  |
  v
chooseCleanExperience(...)
  |-- guided-codex-review
  |-- classic-clean
```

Auto-enter guided Codex cleanup when all of these are true:

- `--guide` is true, or auto-guide conditions are true.
- No explicit category filter was supplied.
- No explicit tool filter was supplied.
- `--risky` was not supplied.
- `--include-active-worktrees` was not supplied.
- `--root` may be supplied. Narrowing scan scope is not a cleanup policy
  override and should not disable auto-guide.
- The scan contains active Codex worktree rows under `.codex/worktrees`.
- The guided planner can build at least one selected or protected row.

Classic cleanup remains the default when any of these are true:

- `--no-guide` is supplied.
- User supplied `--category`.
- User supplied `--tool`.
- User supplied `--risky`.
- User supplied `--include-active-worktrees`.
- There are no active Codex worktrees.
- Codex activity and git safety are both unavailable and no useful protected
  review can be shown.

Add `--no-guide`:

```bash
aibris clean --dry-run --no-guide
```

This should force the existing classic audit output. It is the compatibility
escape hatch for scripts and users who want the old display.

### Stage 2: v0.7.0 TTY Checklist UI

After auto-guide proves the default route is right, improve interaction
quality.

TTY behavior:

- Arrow keys move the cursor.
- Space toggles the current row.
- Enter accepts the selection and renders the normal dry-run plan.
- `q` aborts.
- Header shows selected count, selected size, protected size, scan source, and
  activity source.
- Current row shows full protection or selection reason.
- Rows use stable numbering so docs and fallback mode stay consistent.

Non-TTY behavior:

- Keep line-oriented `[x]` / `[ ]` output.
- Accept number toggles from stdin.
- EOF or blank line accepts the current selection.
- Never require keypress events from a pipe.

Dependency recommendation:

- Use a proven terminal UI library such as Bubble Tea for TTY checklist mode.
- Keep planner, selection state, and dry-run handoff independent of the TTY
  renderer so tests can stay simple.

## Behavior Matrix

| Command | Expected experience |
|---------|---------------------|
| `aibris clean --dry-run` | Auto-guided Codex review when active Codex bloat exists; otherwise classic dry-run |
| `aibris clean` | Auto-guided Codex review, then dry-run-style preview, then final confirmation before deletion |
| `aibris clean --guide --dry-run` | Force guided Codex review |
| `aibris clean --no-guide --dry-run` | Force classic clean audit |
| `aibris clean --category node_modules --dry-run` | Classic clean audit |
| `aibris clean --tool codex --dry-run` | Classic filtered audit, no auto-guide |
| `aibris clean --include-active-worktrees --dry-run` | Classic explicit active-worktree cleanup |
| `aibris clean --risky --dry-run` | Classic risky cleanup audit |
| `printf '\n' \| aibris clean --dry-run` | Non-TTY guided fallback if auto-guide applies; no hang |

## Detailed Requirements

### R1: Mode Decision Is Explicit And Testable

Add a small command-layer function that decides whether to use guided or
classic cleanup. It should take parsed flag state, scan results, and normalized
scan roots. It should not perform deletion or rendering.

Suggested shape:

```go
type cleanExperience string

const (
	cleanExperienceClassic cleanExperience = "classic"
	cleanExperienceGuided  cleanExperience = "guided-codex"
)
```

The decision should use Cobra's `Flags().Changed(...)` API, not inferred
string values.

### R2: `--guide` Keeps Existing Semantics

`--guide` should continue to imply `--category worktree --tool codex` when
category/tool are omitted. Explicit age values must remain respected.

### R3: Default Clean Uses Guided Age Window

When auto-guide triggers and age was not explicitly supplied, use the guided
review age window of `1d`, not the classic `7d`. This is required because the
Codex pressure appears before a week in real use.

When age is explicit, respect it:

```bash
aibris clean --dry-run --age 7d
```

If no category/tool/risky flags are supplied, this may still auto-guide, but
the guide should use the explicit `7d` threshold.

### R4: Existing Classic Path Remains Stable

The classic path must remain byte-for-byte close enough for current tests when
explicit filters are supplied. In particular:

- `--category node_modules`
- `--category worktree`
- `--tool codex`
- `--risky`
- `--include-active-worktrees`
- `--no-guide`

### R5: Guided Output Explains Why It Appeared

Auto-guide should not feel surprising. Add one short line before the guided
table:

```text
guided codex worktree cleanup
  reason     active Codex worktrees are the largest cleanup decision
```

For explicit `--guide`, the reason can be:

```text
  reason     requested by --guide
```

### R6: No Deletion Without Preview

Even when the user runs plain:

```bash
aibris clean
```

guided mode must render the selected target plan before deletion and then ask
for confirmation unless `--force` is supplied.

### R7: Docs Prefer Default Command

Update docs away from "use `--guide`" as the main path.

Primary docs should say:

```bash
aibris clean --dry-run
```

Advanced docs can mention:

```bash
aibris clean --guide --dry-run
aibris clean --no-guide --dry-run
```

## Acceptance Criteria

### v0.6.1

- `aibris clean --dry-run` automatically shows guided Codex cleanup when active
  Codex worktree review candidates exist and no explicit filters are supplied.
- `aibris clean --dry-run --no-guide` shows classic audit output.
- `aibris clean --category node_modules --dry-run` still shows classic audit
  output.
- `aibris clean --tool codex --dry-run` still shows classic filtered audit
  output.
- `aibris clean --include-active-worktrees --dry-run` still uses classic active
  worktree filtering.
- Auto-guide respects explicit `--age`.
- Non-TTY auto-guide accepts EOF or blank input and does not hang.
- No path deletes during `--dry-run`.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass.
- README, SPEC, DOGFOOD, and `skills/aibris/SKILL.md` describe default
  `clean --dry-run` as the recommended path.

### v0.7.0

- TTY mode supports arrow movement, space toggle, Enter preview, and `q` abort.
- Non-TTY fallback remains line-oriented and tested.
- TTY renderer is isolated from planner and cleaner logic.
- The full-screen UI never bypasses dry-run preview or final confirmation.
- Installer smoke confirms the released binary version.

## Test Plan

Unit tests:

- `chooseCleanExperience` chooses guided for unfiltered clean with active Codex
  worktrees.
- `chooseCleanExperience` chooses classic when `--no-guide` is true.
- `chooseCleanExperience` chooses classic for explicit category/tool/risky and
  include-active-worktrees flags.
- Auto-guide default age is `1d` only when age is omitted.
- Explicit `--age=7d` remains `7d`.
- Guided prompt EOF accepts current selection.
- Guided prompt `q` aborts.
- Target normalization still removes nested overlaps.

Command tests:

- `clean --dry-run` with fixture active Codex worktrees prints guided output.
- `clean --dry-run --no-guide` with same fixture prints classic audit.
- `clean --category node_modules --dry-run` remains classic.
- `clean --tool codex --dry-run` remains classic.

Manual dogfood:

```bash
aibris clean --dry-run --root ~/.codex
aibris clean --dry-run --no-guide --root ~/.codex
printf '\n' | aibris clean --dry-run --root ~/.codex
```

## Rollout

### v0.6.1 Patch

Scope:

- Auto-guide decision.
- `--no-guide`.
- Docs update.
- Dogfood transcript refresh.

Release after local validation and GitHub Actions pass.

### v0.7.0 Minor

Scope:

- TTY checklist UI.
- Terminal rendering tests or smoke snapshots.
- Updated docs and dogfood transcript.

Release as minor because it changes the primary interactive experience and may
add a terminal UI dependency.

## Risks And Mitigations

### Risk: Scripts See New Output

`clean` is human output, not a stable machine-readable API. Still, scripts may
exist. Mitigate with `--no-guide` and keep explicit filters classic.

### Risk: Auto-Guide Feels Surprising

Print the reason line and keep the classic escape hatch visible.

### Risk: Guided Mode Selects Active Worktrees

This is expected but sensitive. Keep low-risk default selection conservative,
preserve dry-run preview, and require confirmation before deletion.

### Risk: TTY UI Adds Complexity

Delay full TTY UI to `v0.7.0`. Keep planner and state independent from
rendering so the CLI can fall back cleanly.

## Recommended Issue Split

1. Auto-enter guided cleanup from default `clean`.
2. Add `--no-guide` and classic-path compatibility tests.
3. Refresh docs and dogfood around default `clean --dry-run`.
4. Release `v0.6.1`.
5. Design and implement TTY checklist renderer.
6. Release `v0.7.0`.

## Decision

Recommended: implement Stage 1 immediately as `v0.6.1`.

Reason: it fixes the largest product gap with minimal engineering risk. Users
already have the planner, safety checks, metadata index, and dry-run handoff.
The missing piece is routing the default command to that better decision
surface.
