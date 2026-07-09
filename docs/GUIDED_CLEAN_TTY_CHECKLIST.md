# Guided Clean TTY Checklist Design

Status: design for issue #68, intended to guide the v0.7.0 implementation in
#69.

Source context:

- `docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md`, Stage 2
- `cmd/guided_clean.go`
- `cmd/guided_clean_test.go`
- `docs/SPEC.md`, Guided Codex worktree cleanup

## Goals

- Replace the current number-entry guided prompt with a compact TTY checklist
  when both stdin and stdout are interactive terminals.
- Preserve the line-oriented fallback for pipes, logs, and very small
  terminals.
- Keep the selected target handoff identical: accepted selections render the
  existing dry-run clean plan first, and real deletion still uses the normal
  confirmation path unless `--force` is set.
- Keep planner and selection policy first-party and renderer-independent.

## State Model

The guided clean command should build a renderer-independent selection model
from the existing Codex worktree planner. Each row has:

- stable row number, matching text fallback numbering
- stable item key from project, ID, and path
- item metadata: size, project/name, path, age/status
- planner reason text
- policy class
- selected state
- user override state

Policy classes:

| Class | Default | User action |
| --- | --- | --- |
| `recommended` | selected | Space or number toggle may deselect/reselect |
| `reviewable` | unselected | User may explicitly select if safety policy allows |
| `locked` | unselected | Visible, but cannot be selected |

`recommended` rows are the current low-risk planner selected rows, such as
`zero matching Codex sessions` and `newer Codex activity in same project`.

`reviewable` rows are protected by conservative product policy but have passed
hard safety checks. Examples include newest project worktree, recent activity,
younger than the current age threshold, below size threshold, or no low-risk
signal.

`locked` rows fail hard safety or evidence requirements. Examples include the
current working directory, unavailable Codex activity index, unavailable or
unsafe git status, dirty worktrees, unpushed commits, unknown upstream
comparison, or paths outside the guided Codex worktree convention.

Protected rows remain visible. They are selectable only when their policy class
is `reviewable`; locked rows show the denial reason and stay unselected.

Totals are derived from current state on every render:

- selected count and selected raw size
- protected count and protected raw size for rows not currently selected
- projected freed space from `normalizeCleanTargets(selectedTargets)` so the
  preview total matches the eventual clean plan

## TTY Layout

Use a compact full-screen or near-full-screen TTY view. Do not change scanner or
cleaner output outside the guided prompt.

```text
guided codex worktree cleanup   age > 1d   scan cached, 12s old   activity indexed
selected 3 / 3.1 GB   projected freed 3.1 GB   protected 36 / 30.8 GB

  #    size      project/name              age/status        reason
> [x]  1   1.4 GB  aibris/worktree-a        3d active         zero matching Codex sessions
  [ ]  2   900 MB  aibris/worktree-b        2d active         newest project worktree
  [!]  3   760 MB  harness-stack/current    5d active         current working directory

reason: current working directory
keys: up/down/j/k move  space toggle  enter preview  [/- age down  ]/+ age up  q abort
```

Rendering rules:

- The cursor row is marked with `>`.
- `[x]` means selected.
- `[ ]` means selectable but not selected.
- `[!]` means locked and not selectable.
- Stable numbers never change while the same row remains in the model.
- Rows are sorted by the existing planner order: recommended first by size,
  then unselected rows by size, then stable key ties.
- The footer shows the full reason and full path for the cursor row. This keeps
  rows compact without hiding the decision evidence.
- Long project names and paths are elided in the row, not wrapped. The footer
  may wrap.

For small terminals:

- Below the minimum usable height, fall back to line-oriented mode.
- For narrow width, keep columns in this order: marker, number, size,
  truncated name, reason. Put full path and full reason in the footer.
- The list scrolls; the header and footer remain fixed.
- If rendering cannot initialize cleanly, fall back to text mode instead of
  failing the clean command.

## Key Bindings

| Key | Action |
| --- | --- |
| Up / `k` | Move cursor up |
| Down / `j` | Move cursor down |
| Space | Toggle the cursor row when policy allows it |
| Enter | Accept current selection and render the normal dry-run preview |
| `q` / Esc | Abort without previewing or deleting |
| `[` / `-` | Lower the minimum age threshold to the previous preset |
| `]` / `+` | Raise the minimum age threshold to the next preset |
| `a` | Edit the exact age duration in a one-line prompt |

The age threshold is the minimum age required before a row can be recommended.
Raising it is stricter; lowering it is looser. Use ordered presets around the
current value, for example `6h`, `1d`, `3d`, `7d`, `14d`, and `30d`. If the user
entered a custom `--age`, include that value in the ordered preset list.

Invalid exact age input leaves the threshold unchanged and writes a one-line
status message. It must not accept zero or negative durations.

## Age Recompute

Changing the threshold rebuilds the guided planner state with the new age and
then reapplies user overrides by stable row key only where policy still allows
that override.

Recompute must update:

- selected rows and protected rows
- selected count and selected size
- protected count and protected size
- projected freed space
- cursor row policy and footer reason
- locked/reviewable/recommended markers

If a previously selected row becomes locked, it is deselected and the footer
should explain the new protection reason. If a row moves from protected to
recommended, it becomes selected unless the user had explicitly deselected it
and the row remains selectable.

## Safety Path

The renderer never deletes. Accepting selection returns normalized selected
targets to the existing clean flow:

1. Exit the renderer.
2. Render `printCleanPlan(targets, cleanPlanModeDryRun)`.
3. Print the existing dry-run completion text for `--dry-run`.
4. For real clean, continue to the existing `--interactive`, confirmation, or
   `--force` path.

`--force` may skip the final deletion confirmation, but it must not skip the
preview handoff. `--interactive` keeps the existing per-item deletion prompt
after preview.

If no rows are selected, Enter exits with `No items selected.` and performs no
preview or deletion.

## Non-TTY Fallback

Use fallback mode when stdin or stdout is not a terminal, when the terminal is
too small, or when the TTY renderer cannot initialize.

Fallback keeps line-oriented output:

- `[x]` rows for selected items
- `[ ]` rows for unselected selectable items
- a clear locked marker and reason for non-selectable rows
- stable row numbers shared with the TTY view
- number toggles from stdin, including comma or whitespace separated values
- `q` to abort
- blank line or EOF to accept the current selection

Fallback may also accept `age <duration>`, `+`, `-`, `[`, and `]` line commands
to adjust the age threshold. These commands are optional for scripts; blank
input and EOF must always accept. Fallback must never require raw key events
from pipes.

## Edge States

Empty:

- Forced guide with no rows prints a short empty state and exits on Enter with
  no selected items.
- Auto-guide should normally avoid opening the guided UI when there are no
  useful candidates.

All protected or all active:

- Header shows zero selected plus the protected count and size totals.
- Rows stay visible with reasons.
- Enter exits with no deletion unless the user explicitly selected reviewable
  rows.

All risky or no low-risk signal:

- Nothing starts selected.
- Reviewable rows can be selected if hard safety evidence passes.
- Locked rows cannot be selected.

No activity index:

- Fail closed. Rows are locked with activity unavailable reason.
- The UI must say activity is unavailable and must not read conversation bodies
  to compensate.

Git safety unavailable or unsafe:

- Fail closed per affected row.
- Rows show the git protection reason and cannot be selected.

Narrow `--root`:

- The scan source line shows the narrowed root source.
- The model uses only scanned rows from that root and never widens discovery.
- Empty narrowed roots use the empty state.

Small terminal:

- Prefer text fallback under the minimum usable size.
- Otherwise scroll rows and truncate columns; never let text overlap controls.

## Implementation Boundary

Keep these concerns separate:

- Planner: existing Codex worktree evaluation and safety reasons.
- Selection model: first-party state for rows, cursor, age threshold, user
  overrides, totals, and selected target normalization.
- Renderer: TTY or text presentation and input mapping.
- Cleaner handoff: unchanged dry-run preview, confirmation, and execute path.

The TTY renderer should be replaceable. It should consume and mutate the
selection model through small methods such as move, toggle, set age, accept, and
abort. Unit tests should cover the selection model without terminal event
dependencies.

The current line-oriented `promptGuidedClean` behavior should become or remain
the fallback renderer. It should share the same model and target normalization
as the TTY renderer.

Do not change scanner contracts, cleaner contracts, or classic `clean`
behavior for this UI work.

## Dependency Recommendation And Risks

A proven TUI library such as Bubble Tea is acceptable for #69 because key
parsing, resize handling, alt-screen cleanup, and scrollable lists are easy to
get wrong by hand. The dependency should be isolated to the renderer layer.

What must stay first-party:

- low-risk and protected policy
- hard safety classification
- age threshold recompute behavior
- selected target normalization
- non-TTY fallback behavior
- dry-run and confirmation handoff

Risks:

- dependency size or transitive churn
- terminal behavior differences in CI and user shells
- raw-mode cleanup on panic or interrupt
- accidental hangs when stdin is a pipe
- policy drift between TTY and fallback renderers

Mitigations:

- pin the dependency version when #69 adds it
- keep model tests independent from the TUI library
- run fallback tests with piped stdin, EOF, and blank input
- use text fallback when terminal setup fails
- ensure TTY exit always restores terminal state before printing the dry-run
  plan
