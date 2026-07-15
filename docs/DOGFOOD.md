# Evidence-Based Reclamation Dogfood

These notes record the sanitized evidence used for issue #90. The real `$HOME`
exercise was limited to scan, Git inspection, and dry-run planning. The only
deletion was a disposable branch-backed linked worktree under a temporary
`HOME`; all temporary repositories and worktrees were removed afterward.

## Preserved 2026-07-10 Before Baseline

The v0.7.0 planner treated missing upstream comparison as hard safety. Its
preserved 39-unit, 33.9 GB baseline was:

| Class | Units | Size |
| --- | ---: | ---: |
| Recommended | 3 | 3.1 GB |
| Reviewable | 2 | 0.1 GB |
| Locked | 34 | 30.7 GB |
| Total | 39 | 33.9 GB |

The locked-reason distribution was:

| Reason | Units | Size |
| --- | ---: | ---: |
| Upstream comparison unavailable | 26 | 17.2 GB |
| Dirty files and upstream comparison unavailable | 6 | 11.0 GB |
| Git status unavailable | 2 | 2.5 GB |

Canonical Git common-dir inspection reduced 26 path-derived project labels to
six repositories plus unresolved units. Twelve detached HEADs accounted for
6.0 GB; all twelve were reachable from named refs. Two physical targets had two
nested Git members each, but the old single-member inspector reported them as
unavailable.

## 2026-07-13 Live Read-Only Run

Commands:

```bash
aibris scan --json
printf '\n' | aibris clean --dry-run
printf '\n' | aibris clean --dry-run --age 14d
```

The full scan found 140 items / 20.5 GB. Worktrees accounted for 43 items /
14.7 GB across Codex, Claude, and other convention owners. Guided cleanup
considered the 19 active `.codex` cleanup units / 6.8 GB.

This run happened inside a read-only `$HOME` sandbox, so Codex activity was
unavailable. The planner correctly failed closed instead of substituting weak
evidence or fabricating a recommendation:

```text
guided codex worktree cleanup
  policy     idle>3d, recent<6h locked, keep=3/repo, min-size=256.0 MB

scan
  source     live
  activity   unavailable

summary
  selected   0 items   0 B
  projected  0 B
  protected  19 items   6.8 GB
```

Changing `--age` to `14d` changed only the header to `idle>14d`; the 6-hour
lock and recent-three setting stayed fixed. All rows remained locked because
activity evidence was unavailable, not because age replanning silently changed
another safety input.

### Sanitized Git Evidence

Read-only member inspection produced this distribution. Repository names and
paths are replaced by stable aliases.

| Evidence | Distribution |
| --- | --- |
| Canonical repositories | 5 groups containing 8, 4, 4, 2, and 1 units |
| Unit member count | 19 one-member units; no live multi-member unit remained |
| Worktree state | 11 clean, 8 dirty or untracked |
| HEAD state | 8 attached, 11 detached |
| Detached reachability | 11 reachable from named refs, 0 unreferenced |
| Attached upstream | 4 configured, 4 missing or gone |

The corresponding visible reason counts were:

| Reason or metadata | Rows | Policy effect |
| --- | ---: | --- |
| Activity evidence unavailable | 19 | Hard lock |
| Dirty or untracked files | 8 | Additional hard lock |
| Detached HEAD unreferenced | 0 | No live occurrence; hard lock is fixture-tested |
| Upstream missing or gone | 4 | Explanation only; zero rows locked solely for upstream |

This live state had drifted below the accepted 10 GB recommendation bar because
required activity evidence was unavailable. The observed recommendation remains
truthfully 0 B.

## Deterministic Accepted-Baseline Fixture

`TestEvidenceBasedReclamationBaseline` preserves the accepted 39-unit /
33.9 GB shape without depending on mutable local state:

| Decision | Units | Size |
| --- | ---: | ---: |
| Hard locked | 8 | 13.5 GB |
| Recent-three retained | 11 | 6.7 GB |
| Age or size hold | 7 | 0.2 GB |
| Recommended | 13 | 13.5 GB |
| Total | 39 | 33.9 GB |

The fixture includes six dirty units, one unreferenced detached HEAD, one unit
with unavailable Git evidence, an attached branch with no upstream, a detached
HEAD reachable from a named remote ref, and a safe two-member cleanup unit. It
asserts that:

- all dirty, unreferenced-detached, and unavailable-evidence units remain
  locked;
- missing upstream does not lock an otherwise safe unit;
- the two-member unit is inspected and recommended as one physical target;
- canonical repository IDs, rather than display names, drive recent-three
  retention;
- recommended bytes are 13.5 GB, above the 10 GB acceptance threshold; and
- raising minimum idle age changes recommendations while leaving hard locks and
  recent-three retention unchanged.

Temporary-Git integration tests separately discover both nested `.git` members,
aggregate a dirty member into a unit lock, preflight every member before
mutation, and verify partial receipts do not claim freed bytes. This guards the
member-inspection seam rather than only feeding prebuilt members to the policy.

Run it with:

```bash
go test ./cmd -run TestEvidenceBasedReclamationBaseline -count=1
```

## Disposable Git-Aware Removal

A temporary repository was initialized with `main`, then a local-only branch
`preserve-me` was checked out as a linked worktree under
`<temp-home>/.codex/worktrees/disposable`. The worktree was made old enough for
the classic selector and previewed first:

```bash
HOME=<temp-home> aibris clean \
  --root <temp-home>/.codex \
  --category worktree \
  --include-active-worktrees \
  --age 1h \
  --dry-run
```

Sanitized preview:

```text
policy  age>1h, risky=false, active-worktrees=included
eligible   1 item   4.0 KB
matched    1 candidate   4.0 KB
[DRY-RUN] No files were removed.
```

The same command without `--dry-run` used `--force` only to make this automated
fixture non-interactive. The executor did not pass force to Git:

```text
removing worktree member 1/1: <temp-home>/.codex/worktrees/disposable ...
removed worktree member: <temp-home>/.codex/worktrees/disposable

worktree execution receipt
  unit      removed <temp-home>/.codex/worktrees/disposable
    member  removed <temp-home>/.codex/worktrees/disposable
    physical-removed true   freed 4.0 KB
```

Postconditions:

| Check | Before | After |
| --- | --- | --- |
| `refs/heads/preserve-me` | captured OID | same OID |
| Physical member path | exists | absent |
| Parent `git worktree list` | parent + disposable member | parent only |
| Unrelated `main` worktree | present | present |

This exercises the shipped preflight, non-forced `git worktree remove`, branch
verification, metadata verification, and receipt path. No real user worktree was
deleted.
