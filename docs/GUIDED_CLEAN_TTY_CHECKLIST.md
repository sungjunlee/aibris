# Guided Clean Checklist And Policy

Status: shipped selection and policy contract through evidence-based worktree
reclamation (#82-#90), with worktree-wide tool admission from #141.

Source of truth:

- `cmd/guided_clean.go`
- `cmd/worktree_cleanup_units.go`
- `cmd/worktree_git_evidence.go`
- `cmd/worktree_activity.go`
- `cmd/worktree_cleanup_policy.go`
- `cmd/worktree_executor.go`
- `docs/SPEC.md`

## Entry And Routing

Plain `clean` and `clean --dry-run` enter guided worktree review when all of these
are true:

- no explicit classic selector or `--no-guide` is present;
- at least one validated active `worktree` cleanup unit exists; and
- active worktree pressure across all scanner tools is at least 256 MB or three
  units.

Routing uses physical-unit pressure, not selected-row count. A protected-only
state therefore opens the checklist with zero selected rows instead of falling
back to the classic `active worktree protected` summary.

`--category`, `--tool`, `--risky`, `--include-active-worktrees`,
`--interactive`, and `--force` are classic selectors unless `--guide` is also
explicit. `--root` and `--age` do not disable automatic guided routing.
Explicit `--guide` defaults only an omitted category to `worktree`; it never
implies `--tool codex`. An explicit `--tool` narrows guided inventory normally.

## Cleanup Unit Model

One guided checklist row represents one canonical physical worktree target. At
the all-category handoff, exact-path and nested discoveries remain attached as
logical evidence, while the outermost executable owner alone supplies size and
projected freed bytes. The target may contain a direct Git worktree or several
one-level nested Git members.

Admission requires `CategoryWorktree` and active status, not a particular tool.
The row preserves its representative scanner `Tool`, `Source`, identity,
project, physical target, and size. A missing-representative fallback is
deterministic and tool-neutral rather than Codex-shaped. `unknown` is a valid
guided tool; non-worktree rows, installed content, and retention projection
rows are absent.

Every member records:

- canonical, symlink-resolved Git common-dir repository ID;
- display repository name;
- HEAD OID and attached branch ref, if any;
- local and remote named refs containing HEAD;
- upstream metadata for explanation only;
- dirty/untracked and recoverability state; and
- activity evidence.

All members must pass hard safety. A multi-member unit is retained when it ranks
in the recent set for any member repository. Member-specific reasons include the
member basename in rendered text.

## Hierarchical Policy

The displayed policy is:

```text
policy     idle>3d, recent<6h locked, keep=3/repo, min-size=256.0 MB
```

Evaluation order is fixed:

1. Hard lock.
2. No registered activity source: explicit reviewable state.
3. Recent-three retention by canonical repository.
4. Minimum idle age.
5. Minimum recommendation size.
6. Recommendation.

Hard locks are:

- the current working directory is the unit or below it;
- any member is dirty or has untracked files;
- Git identity or recoverability evidence is unavailable;
- a registered activity source is unavailable or unreadable;
- a detached HEAD is unreachable from every named local and remote ref; or
- activity from a healthy registered source is within the fixed 6-hour safety
  window.

An attached local branch is recoverable even when no upstream is configured or
the upstream is gone. A detached HEAD is recoverable when a named local or
remote ref contains it. Upstream state is appended to the explanation but does
not create a lock.

For tools with healthy registered activity evidence, retention ranks every unit
by last activity, then stable target key, within each canonical repository. The
newest three are reviewable. Locked units still occupy retention positions; the
planner does not backfill a fourth unit.

Structural absence of an activity source is not the unavailable-source lock. An
otherwise safe row with no registered source is reviewable, initially
unselected, and has the exact reason `activity source not registered for this
tool`. It may be manually selected, but changing age can never make it
recommended, initially selected, or projected-freed without that selection.

For rows with healthy activity evidence, the guided minimum idle age defaults
to 3 days. A safe unit younger than that is reviewable. A safe older unit below
256 MB is also reviewable. Only a safe, non-retained, old-enough, large-enough
unit is recommended. Codex retains its existing activity semantics, including
the fixed six-hour lock and recent-three policy.

## Selection State

| Class | Default | Number toggle |
| --- | --- | --- |
| `recommended` | selected | may deselect and reselect |
| `reviewable` | unselected | may explicitly select |
| `locked` | unselected | denied |

Markers are `[x]`, `[ ]`, and `[!]` respectively. Rows sort by recommendation
first, then size, then stable cleanup-unit key. Mixed-tool row numbers remain
stable during age replanning.

Totals are recomputed from current state:

- selected count and raw size;
- projected freed bytes after physical-target normalization; and
- protected count and size for every unselected row.

## Shipped Text And TTY Interaction

The current TTY and non-TTY paths share the line-oriented selection model. TTY
output identifies `mode tty checklist`; piped output omits that line. Both accept:

| Input | Action |
| --- | --- |
| `1`, `1 3`, `1,3` | toggle selectable row numbers |
| `age 7d` | set exact minimum idle age |
| `+` or `]` | move to the next stricter age preset |
| `-` or `[` | move to the next looser age preset |
| blank line or EOF | accept selection and continue |
| `q` | abort |

Presets are `6h`, `1d`, `3d`, `7d`, `14d`, and `30d`, plus an explicit custom
value. Invalid, zero, or negative ages leave state unchanged.

Age replanning changes only `MinIdleAge`. It must preserve the 6-hour recent
window, recent-three ranking, Git evidence, and activity evidence. Explicit
user selection overrides survive while a row remains selectable. If a row is
locked, it is deselected and any override is cleared. A no-source row retains
its explicit reviewable reason and never gains an automatic selection override.

## Preview And Execution Handoff

The checklist never deletes. Acceptance follows this path:

1. Normalize selected physical targets.
2. Render the normal dry-run clean plan.
3. If `--dry-run` is set, retain the selected guided parents for overlap
   normalization without deleting them.
4. Otherwise prepare active-unit identity before confirmation.
5. Ask the normal final confirmation unless `--interactive` or `--force`
   controls that prompt.
   A missing or declined confirmation aborts the whole command before the
   classic phase.
6. Rebuild and compare all active member evidence immediately before mutation.
7. Remove active members with non-forced Git worktree semantics and verify refs,
   paths, and parent metadata.
8. Continue to the classic all-category audit. During dry-run, normalize
   classic targets together with selected guided parents so nested paths are
   shown as zero-byte evidence rather than previewed as additional physical
   targets.

`--force` skips only final confirmation. It does not select locked rows and is
never translated into `git worktree remove --force`.

Selected non-Codex and unknown-tool rows use this exact prepared-target,
overlap, deletion-time preflight, non-forced removal, verification, and receipt
path. Overlap audit reasons are attached to every guided active worktree, not
only Codex rows.

For multi-member units, preflight completes for every member before the first
removal. An execution-time partial failure stops the unit, reports each removed
and remaining member, preserves the remaining physical container, and credits
no bytes unless the full target is gone. Active Git removal failure never falls
back to raw recursive deletion.

## Edge States

Empty guided state:

- Render the empty sections.
- Blank input or EOF returns `No items selected.` and continues to the classic
  all-category audit.

Protected-only pressure:

- Open guided review when the pressure threshold is met.
- Show zero selected and every reason.
- Blank input or EOF performs no guided worktree preview or deletion, then
  continues to classic candidates.

Activity source states:

- Lock a row whose registered activity source is unavailable or unreadable with
  the existing `activity evidence unavailable` reason.
- Keep a row whose tool has no registered source reviewable and initially
  unselected with `activity source not registered for this tool`.
- Do not inspect conversation bodies or substitute project labels for activity.

Classic override:

- Keep classic audit and filtering behavior.
- If active worktrees are explicitly included, reuse cleanup-unit Git safety and
  the same Git-aware executor.

Narrow root:

- Use only rows discovered under validated `$HOME` roots.
- Never widen discovery while building guided state.
