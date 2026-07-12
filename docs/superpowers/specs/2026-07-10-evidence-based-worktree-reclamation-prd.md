# Evidence-Based Worktree Reclamation PRD

Date: 2026-07-10
Status: Accepted and registered
Target release: v0.8.0 candidate
GitHub milestone: [#5 Evidence-Based Worktree Reclamation](https://github.com/sungjunlee/aibris/milestone/5)
Planning PR: [#79 Define evidence-based worktree reclamation](https://github.com/sungjunlee/aibris/pull/79)

## Summary

`aibris v0.7.0` makes guided Codex cleanup discoverable and reviewable, but the
planner still protects most reclaimable space for the wrong reason. A worktree
is called `active` whenever its Git metadata is linked, and missing upstream
configuration is treated as a hard safety failure. On the current dogfood
machine this locks 34 of 39 guided rows and 30.7 GB of 33.9 GB, even though no
matching worktree has recent 24-hour Codex activity and every detached HEAD is
reachable from a named ref.

The next product move is to replace proxy-based protection with evidence-based
reclamation:

1. Model a physical cleanup target separately from the Git worktree members it
   contains.
2. Identify repositories by canonical Git common-dir rather than path-derived
   project labels.
3. Lock only work that is live, dirty, unreadable, or not recoverable from a
   named ref.
4. Retain the three most recent cleanup units per repository as an overridable
   recovery convenience.
5. Recommend old, large, recoverable units even when their upstream is gone.
6. Remove active worktrees through a Git-aware executor that preserves branch
   refs and fails closed.

This PRD is intended to be the source for one milestone, two implementation
epics, and independently verifiable leaf issues.

## Decision

Adopt a hierarchical cleanup policy with three independent time and retention
concepts:

- `RecentActivityWindow = 6h`: hard safety lock.
- `KeepPerRepository = 3`: reviewable retention, ranked by meaningful activity.
- `MinIdleAge = 3d` by default: minimum idle duration before automatic
  recommendation; explicit `--age` overrides only this value.

Replace upstream availability with ref reachability:

- A clean named branch is recoverable even when it has no upstream.
- A detached HEAD is recoverable when its commit is reachable from any named
  local or remote ref.
- A detached HEAD that is not reachable from a named ref remains locked.

Do not broaden deletion authority. Guided recommendations still require the
existing preview and confirmation flow, and `--dry-run` remains delete-free.

## Current State

### Shipped Product

`v0.6.0` introduced the Codex activity index, conservative Git checks, and
guided selected/protected rows. `v0.6.1` routed plain `clean` into guided review
when recommendations exist. `v0.7.0` added recommended, reviewable, and locked
selection classes plus age replanning and a TTY checklist.

Those layers should be retained:

- metadata-only Codex activity indexing
- selected target normalization
- recommended/reviewable/locked row classes
- TTY and text renderers sharing one selection model
- dry-run preview before any deletion
- final confirmation unless explicitly skipped
- classic behavior for explicit cleanup selectors and `--no-guide`

The remaining problem is the evidence and policy feeding those layers.

### Live Dogfood Baseline

The 2026-07-10 baseline was produced from `origin/main` after `v0.7.0` using a
fresh full-home scan and the current Codex activity index.

| Planner class | Count | Size |
| --- | ---: | ---: |
| Recommended | 3 | 3.1 GB |
| Reviewable | 2 | 0.1 GB |
| Locked | 34 | 30.7 GB |
| Total | 39 | 33.9 GB |

Locked reasons:

| Reason | Count | Size |
| --- | ---: | ---: |
| Upstream comparison unavailable | 26 | 17.2 GB |
| Dirty files and upstream comparison unavailable | 6 | 11.0 GB |
| Git status unavailable | 2 | 2.5 GB |

Additional evidence:

- 34 rows / 30.7 GB have no configured upstream.
- 20 rows / 22.2 GB have a named branch but no upstream.
- 18 of those named-branch rows / 15.9 GB are clean.
- 12 detached rows / 6.0 GB are all reachable from a named ref.
- 11 of the 12 detached rows are reachable from a remote ref.
- Zero detached rows are unreferenced.
- Zero matching worktree sessions occurred in the latest 6 or 24 hours.
- Two physical cleanup targets contain two nested `.git` members each; the
  current single-member Git inspector reports both as unavailable.
- Path-derived scan labels report 26 projects, while canonical Git common-dir
  inspection resolves the rows to six repositories plus unresolved units.

Changing the guided age threshold between `6h`, `1d`, `3d`, `7d`, and `14d`
does not change the current plan: 3 recommended, 2 reviewable, and 34 locked.
The upstream lock dominates every age or activity decision.

### Counterfactuals

Removing all Git safety locks without replacing them would make the current
planner recommend 19 rows / approximately 27.5 GB. That is too aggressive
because dirty and genuinely unrecoverable states would lose hard protection.

Applying this PRD's proposed policy to the same baseline produces the following
planning estimate before implementation:

| Proposed class | Count | Size |
| --- | ---: | ---: |
| Hard locked | 8 | 13.5 GB |
| Recent-3 retained | 11 | 6.7 GB |
| Age or size hold | 7 | 0.2 GB |
| Recommended | 13 | 13.5 GB |

The estimate keeps the six dirty units and two structurally ambiguous
multi-member units locked. It treats currently referenced detached HEADs as
recoverable and uses canonical repository grouping for retention. The exact
shipped result may differ after multi-member inspection replaces the current
ambiguity.

## Problem

The current planner conflates five distinct concepts:

1. **Linked health**: the `.git` file still points to parent repository
   metadata.
2. **Physical cleanup identity**: the directory that will be removed to reclaim
   bytes.
3. **Logical Git membership**: one or more actual Git worktrees contained in
   that directory.
4. **Recoverability**: whether committed state survives removal through a named
   ref.
5. **Liveness**: whether a human or Codex session is using the worktree now.

Because these concepts are collapsed into one `DebrisInfo` row and one
`active` status, the policy uses misleading proxies:

- Linked metadata is treated as proof of current use.
- Missing upstream is treated as proof of data-loss risk.
- A parent directory mtime is treated as worktree activity.
- A path-derived project label is treated as repository identity.
- More than one nested `.git` member is treated as unknowable.
- The same `Age` value controls both minimum idle age and recent-session
  protection.

The result is safe in the narrow sense that little is deleted, but it fails the
user job: reclaim substantial Codex worktree space while clearly preserving
work that is live or not recoverable.

## User Jobs

### Developer Reclaiming Disk

> Show me old Codex worktree directories I can remove without losing current
> edits or unique commits, while keeping a small set I am most likely to resume.

Expected entrypoint:

```bash
aibris clean --dry-run
```

### Developer Reviewing A Local-Only Branch

> My remote branch was deleted after merge, but the local branch still exists.
> Tell me that the branch will remain, and let the old worktree be recommended
> if it is otherwise idle and outside retention.

### Developer With Concurrent Work

> Never preselect a worktree that is dirty, recently active, current, or holding
> a detached commit that cannot be recovered from a named ref.

### AI Assistant

> Produce deterministic, explainable recommendations from metadata and Git
> evidence, then ask for explicit approval before deletion.

## Goals

- Make `active` Codex worktree reclamation materially useful without weakening
  data-loss protections.
- Separate physical cleanup targets from contained Git worktree members.
- Use canonical repository identity for grouping and retention.
- Replace upstream configuration checks with explicit recoverability evidence.
- Separate recent-activity safety, per-repository retention, and idle-age
  recommendation thresholds.
- Preserve branch refs when removing active worktrees.
- Make every recommendation and lock explainable through stable reason codes.
- Keep `scan --json`, classic clean selectors, and the v0.7.0 selection UI
  stable unless a later compatibility decision explicitly changes them.
- Produce issue-sized implementation slices with objective acceptance criteria.

## Non-Goals

- Do not delete local or remote branch refs.
- Do not garbage-collect repository objects or reflogs.
- Do not remove Codex sessions, archived sessions, or conversation bodies.
- Do not read conversation bodies to infer intent or liveness.
- Do not redesign the v0.7.0 checklist renderer.
- Do not add automatic deletion without preview and confirmation.
- Do not make `--force` bypass hard safety; it only skips final human
  confirmation after planning and preview.
- Do not generalize the first release to every AI tool's session format. The
  cleanup-unit and Git evidence model may be reusable, but Codex activity is the
  initial product scope.
- Do not require a persisted `scan --json` schema change in v0.8.0.
- Do not delete stale local branches after worktree removal.

## Product Principles

### Protect Irrecoverable State, Not Missing Configuration

Upstream tracking is a workflow convenience. It is not the definition of
whether committed work survives worktree removal. Safety should answer whether
unique state remains reachable after the cleanup action.

### Activity, Retention, And Eligibility Are Different Policies

Recent activity is a hard safety signal. Recent-per-repository retention is a
returnability convenience. Idle age is an eligibility threshold. A single
duration must not control all three.

### A Cleanup Target Is Not Necessarily One Git Worktree

The physical directory is the deletion and size-accounting unit. Every Git
worktree inside it is a safety member. The target may be recommended only when
all members pass hard safety.

### Default Recommendations Should Be Useful And Reversible

The best path remains plain `clean --dry-run`. Recommended rows should reclaim
meaningful space, keep branch refs intact, and remain subject to user review.

### Fail Closed On Missing Evidence, Not On Known Safe Variants

Unreadable Git state remains locked. A clean named branch without upstream is
not missing evidence: its local ref is known and recoverable.

## Terminology

| Term | Meaning |
| --- | --- |
| Linked worktree | A worktree whose `.git` metadata still resolves to its parent repository. Existing internal status may remain `active` for compatibility. |
| Cleanup unit | One physical path presented, sized, selected, and removed as a single cleanup target. |
| Git member | An actual Git worktree contained directly or under a cleanup unit. |
| Repository ID | Canonical, symlink-resolved Git common-dir path used internally for grouping. |
| Named ref | A local or remote ref that makes a commit reachable after worktree removal. |
| Last activity | Maximum trusted timestamp from Codex session metadata, per-worktree HEAD reflog, and safe fallback metadata. |
| Locked | Cannot be selected because deletion could lose live or unrecoverable state, or evidence is unavailable. |
| Retained | Safe enough to review but default-unselected because it is among the recent three for a repository. |
| Recommended | Passes hard safety, retention, idle-age, and size policy and starts selected. |
| Reviewable | Safe enough to select manually but held by soft policy such as retention, age, or size. |

## Proposed Domain Model

The guided path should build an internal cleanup model after scan and before
planning. It may adapt existing `DebrisInfo` rows without changing the persisted
scan schema.

```go
type WorktreeCleanupUnit struct {
    TargetPath string
    Size       int64
    Source     string
    Members    []GitWorktreeMember
}

type GitWorktreeMember struct {
    WorktreePath      string
    RepositoryID     string
    DisplayRepository string
    BranchRef         string
    HeadOID           string
    ReachableRefs     []string
    Dirty             bool
    EvidenceAvailable bool
    LastActivity      time.Time
}

type WorktreeCleanupDecision struct {
    Unit    WorktreeCleanupUnit
    Class   DecisionClass
    Reasons []DecisionReason
}
```

The external seam should be small:

```go
func BuildWorktreeCleanupUnits(items []types.DebrisInfo) ([]WorktreeCleanupUnit, error)
func PlanWorktreeCleanup(units []WorktreeCleanupUnit, policy CleanupPolicy) CleanupPlan
```

Git inspection, activity collection, and multi-member aggregation are internal
implementation details behind these interfaces. Callers and tests should assert
the plan, not reconstruct evidence rules independently.

## Evidence Model

### Physical Cleanup Identity

- Canonicalize the scanner-provided target path.
- Group duplicate rows by target path before size and policy totals.
- Enumerate every direct or one-level nested `.git` file already accepted by
  worktree discovery.
- Treat the group as one cleanup unit with one or more Git members.
- Do not discard additional nested members during deduplication.
- If any discovered member cannot be inspected, lock the entire unit.

### Repository Identity

- Resolve each member's Git common-dir to a canonical absolute path.
- Use that path, or a stable hash of it, as the internal repository ID.
- Use the repository directory basename only for display.
- Two repositories with the same basename must remain distinct.
- A multi-member unit may belong to multiple repositories.
- Retain the unit when it is in the retained set for any member repository.

### Git Recoverability

For each member:

1. Read porcelain status and reject dirty or untracked content.
2. Resolve HEAD and whether it is attached to a named local branch.
3. Enumerate local and remote refs containing HEAD.
4. Mark HEAD recoverable when either:
   - HEAD is attached to a named local branch, or
   - detached HEAD is reachable from at least one named local or remote ref.
5. Lock a detached HEAD that is not reachable from any named ref.
6. Record upstream presence only as explanatory metadata.

Missing or gone upstream must not independently lock a unit.

The planner may explain recoverability with reasons such as:

- `local branch retained`
- `remote ref contains HEAD`
- `detached HEAD reachable from named ref`
- `detached HEAD not reachable from named ref`
- `dirty or untracked files`
- `git evidence unavailable`

### Last Activity

Compute member last activity from the maximum available trusted timestamp:

1. Latest matching Codex session metadata timestamp.
2. Latest per-worktree HEAD reflog timestamp.
3. Existing entry/worktree metadata timestamp as a fallback.

Do not recursively walk file mtimes to calculate activity. Dirty status already
protects uncommitted file changes, and full-tree timestamp scans would add cost
without reliably identifying user intent.

The unit's last activity is the maximum activity across all members.

When the activity index is unavailable:

- Git and fallback metadata may still identify old state, but the absence of
  Codex evidence must be explicit.
- The first implementation should fail closed for automatic recommendation
  until tests and dogfood establish a narrower safe fallback.
- Reviewable rendering may still explain the unit and missing evidence.

## Cleanup Policy

Policy evaluation is ordered. The first hard-safety failure locks the unit;
soft policy is evaluated only after every member passes hard safety.

### 1. Hard Locks

Lock a cleanup unit when any of these are true:

- The current working directory is the unit or a descendant of it.
- Any member has dirty or untracked files.
- Any member's Git evidence cannot be read or is structurally ambiguous after
  full member enumeration.
- Any detached HEAD is not reachable from a named ref.
- Unit last activity is within `RecentActivityWindow`, default `6h`.
- Codex activity evidence required by the current release policy is unavailable.
- The unit path falls outside validated Codex worktree conventions.

Locked rows use `[!]`, remain visible, and cannot be selected. Neither checklist
toggle nor CLI `--force` may override them.

### 2. Per-Repository Retention

For every canonical repository:

- Rank cleanup units by last activity descending.
- Break ties by stable cleanup-unit key.
- Mark the first `KeepPerRepository`, default `3`, as retained.
- A unit linked to several repositories is retained when it ranks in the top
  three for any repository.
- Rank all units before class assignment. A hard-locked unit may occupy one of
  the three positions, remains locked, and does not cause a fourth unit to be
  backfilled into retention.

Retained rows are reviewable and default-unselected. Users may deliberately
select them because hard safety has already passed.

### 3. Idle Age

- `MinIdleAge` defaults to `3d` for guided Codex cleanup.
- Explicit `--age` overrides only `MinIdleAge`.
- Checklist age controls replan only idle eligibility.
- Changing age must not change the `6h` hard activity window or the recent-three
  retained set except when new evidence is collected.
- Units younger than `MinIdleAge` are reviewable and default-unselected.

### 4. Size Threshold

- Keep the existing `256 MB` default recommendation threshold.
- Units below the threshold remain reviewable.
- Size must be counted once per physical cleanup unit.

### 5. Recommendation

Recommend and default-select a unit only when:

- all Git members pass hard safety
- it is outside recent activity lock
- it is outside per-repository retention
- it is older than `MinIdleAge`
- it is at least the minimum recommendation size

A clean named branch with missing or gone upstream may be recommended. The
reason must state that the branch ref is retained.

### Decision Precedence

```text
hard safety failure
  -> locked
else recent-three in any repository
  -> reviewable: retained
else younger than MinIdleAge
  -> reviewable: age hold
else below minimum size
  -> reviewable: size hold
else
  -> recommended
```

The current indefinite `no low-risk Codex signal` protection is removed. A
historical session remains useful as activity evidence but does not protect a
unit forever.

## Git-Aware Execution

Relaxing planner locks requires an explicit active-worktree execution contract.
The current generic path removal is not sufficient because it can leave stale
parent-repository worktree metadata.

### Preflight

Immediately before execution:

- Rebuild or refresh Git safety for every selected active unit.
- Verify every selected member still resolves to the same repository and HEAD.
- Recheck dirty status, CWD containment, and ref reachability.
- Abort the entire unit before mutation if any member no longer passes.

### Removal

- Remove each active Git member through a Git-aware `git worktree remove` path.
- Do not delete the associated local branch ref.
- Do not translate aibris `--force` into Git worktree removal force.
- If Git-aware removal fails, report the failure and do not fall back to raw
  recursive deletion for that active member.
- After every member is removed successfully, remove the now-unused physical
  container and residual disposable content through the existing safe-path
  checks.
- Orphaned units may continue to use path removal because their parent Git
  metadata no longer exists.

### Multi-Member Units

- Preflight every member before removing any member.
- If preflight fails for one member, remove none of the unit.
- If an execution-time failure occurs after partial progress, stop the unit,
  report exactly which members were removed, and preserve the remaining path.
- Cleanup receipts must not claim the full unit size when the full physical
  target remains.

### Postconditions

For a successfully removed active unit:

- the physical target no longer exists
- parent `git worktree list` no longer references removed member paths
- every branch ref captured during preflight still resolves to the same OID
- no unrelated worktree metadata or branch ref is changed

## Guided UX Requirements

Reuse the v0.7.0 checklist and selection model. This PRD changes policy and
evidence, not renderer interaction.

The header should expose independent policy values:

```text
policy  idle>3d, recent<6h locked, keep=3/repo, min-size=256MB
```

Rows should use precise reason language:

```text
[x] old-feature       local branch retained; idle 14d; outside recent 3
[ ] recent-feature    retained: repository rank 2 of 23
[!] current-work      recent activity 42m ago
[!] detached-unique   detached HEAD not reachable from a named ref
```

Age replanning must affect only idle-age holds and recommendations. Locked rows
and retained ranking remain stable unless underlying evidence changes.

### Default Guided Routing

Plain `clean` should open guided review when Codex worktree review is valuable,
even when zero rows start recommended. The current selected-row-only routing
can hide all-protected evidence behind the classic `active worktree protected`
summary.

Treat guided review as valuable when:

- no explicit classic selector or `--no-guide` is supplied
- at least one validated active Codex cleanup unit exists
- total active Codex cleanup pressure is at least `256 MB` or three units

Explicit classic selectors, `--no-guide`, and existing non-TTY behavior remain
unchanged.

## Behavior Matrix

| Scenario | Expected result |
| --- | --- |
| Clean named branch, upstream gone, idle 14d, outside recent 3 | Recommended; branch-retained reason |
| Clean named local-only branch, idle 14d, outside recent 3 | Recommended; branch-retained reason |
| Detached HEAD reachable from remote ref, idle 14d | Eligible for retention/age/size policy |
| Detached HEAD reachable only from local named ref | Eligible; local ref retained |
| Detached HEAD unreachable from named refs | Locked |
| Dirty or untracked member | Entire cleanup unit locked |
| One cleanup unit with two members, both safe | One row, one size, aggregate policy |
| One cleanup unit with two members, one unsafe | Entire unit locked with member reason |
| Latest activity 2h ago, age threshold 3d | Locked by recent activity |
| Latest activity 10h ago, repository rank 2 | Reviewable retained row |
| Latest activity 10h ago, repository rank 4, unit age 2d | Reviewable age hold |
| Latest activity 4d ago, repository rank 4, 1 GB | Recommended |
| Activity index unavailable | Locked for automatic recommendation in initial release |
| `--age 7d` | Changes idle eligibility only; recent window remains 6h |
| Plain clean with only locked high-pressure Codex units | Guided review opens with zero selected |
| `--force` on a locked row | Row remains unselectable |

## Detailed Requirements

### R1: Build Physical Cleanup Units

- Group scanner rows by canonical target path.
- Enumerate every actual Git member under each target using the same bounded
  discovery contract as the adapter.
- Preserve all members through planning and execution.
- Count target size once.
- Return deterministic unit keys and member order.

### R2: Resolve Canonical Repository Identity

- Resolve symlinks and Git common-dir for every member.
- Use canonical identity for activity grouping and recent-three retention.
- Never group solely by display basename or path-derived project label.
- Support one cleanup unit referencing multiple repositories.

### R3: Collect Recoverability Evidence

- Report dirty/untracked state, attached branch, HEAD OID, containing local
  refs, containing remote refs, and evidence failures.
- Missing upstream is informational, not a hard failure.
- Detached reachability uses named refs, not upstream.
- Evidence collection respects context cancellation and bounded command time.

### R4: Collect Meaningful Activity

- Reuse metadata-only Codex session indexing.
- Add per-worktree HEAD reflog activity.
- Use fallback metadata only when stronger evidence is absent.
- Return one last-activity timestamp per member and unit.
- Keep conversation bodies unread.

### R5: Implement Hierarchical Policy

- Apply hard lock, retention, idle age, size, and recommendation in documented
  order.
- Keep recent window, retention count, idle age, and minimum size as separate
  policy fields injectable in tests.
- Default to `6h`, `3`, `3d`, and `256 MB` respectively.
- Replace indefinite no-low-risk protection with retained ranking and idle age.
- Produce stable reason codes plus human descriptions.

### R6: Reclassify Guided Rows

- Hard safety failures map to locked.
- Recent-three, age, and size holds map to reviewable.
- Eligible units map to recommended and selected.
- Upstream absence never maps directly to locked.
- Recent activity maps to locked, not reviewable.

### R7: Make Age Replanning Orthogonal

- Existing `--age`, TTY age controls, and text commands modify only
  `MinIdleAge`.
- Replanning preserves user overrides when the row remains selectable.
- Replanning cannot unlock recent activity or unrecoverable HEAD state.
- Output displays all policy values independently.

### R8: Add Git-Aware Active Execution

- Preflight active units immediately before mutation.
- Remove active members through Git worktree semantics.
- Preserve branch refs and verify postconditions.
- Refuse raw recursive fallback after Git-aware failure.
- Keep orphaned path cleanup behavior stable.

### R9: Support Multi-Member Execution And Receipts

- Aggregate safety across all members.
- Preflight the full unit before mutation.
- Report partial execution truthfully.
- Credit freed bytes only when the physical target is actually removed.

### R10: Route High-Pressure Protected-Only States To Guided Review

- Default guided routing uses cleanup pressure and available decision rows, not
  selected count alone.
- Explicit selectors and `--no-guide` preserve classic behavior.
- Non-TTY input never hangs.
- Zero selected rows exit without preview or deletion unless the user selects a
  reviewable row.

### R11: Preserve Compatibility Surfaces

- Keep `scan --json` compatible.
- Keep classic `clean` filter behavior for explicit selectors.
- Keep `--dry-run` delete-free.
- Keep confirmation semantics.
- Keep v0.7.0 TTY/text input contracts.

### R12: Document And Dogfood The Policy

- Update SPEC, README, skill workflow, checklist design, and dogfood notes.
- Explain linked versus recently active semantics.
- Capture reason distribution before and after on a real local scan.
- Verify branch refs and parent worktree metadata after a controlled removal.

## Acceptance Criteria

### Planning And Evidence

- A direct worktree and a nested worktree produce correct cleanup units.
- A target containing two nested Git members produces one unit with two
  members, one size, and aggregate safety.
- Canonical common-dir identity groups differently named worktree paths from the
  same repository.
- Repositories with the same display basename remain distinct.
- A clean named branch with no upstream is not locked.
- A detached HEAD reachable from a named ref is not locked by detachment alone.
- A detached HEAD unreachable from any named ref is locked.
- Dirty/untracked state locks the entire unit.
- CWD containment locks the entire unit.
- Missing Git evidence locks the entire unit.

### Activity And Retention

- Activity within 6 hours is locked regardless of `--age`.
- The three most recent units per canonical repository are reviewable and
  default-unselected after hard safety passes.
- The fourth old, large, safe unit becomes recommended when idle age passes.
- A multi-repository unit is retained when it ranks in the recent three for any
  member repository.
- `--age` changes idle eligibility without changing the 6-hour lock or recent
  three ranking.
- Historical session existence alone does not protect a unit forever.

### Execution

- Active removal preserves attached local branch refs and OIDs.
- Referenced detached HEAD commits remain reachable after removal.
- Git-aware removal failure does not fall back to raw active-path deletion.
- A failed multi-member preflight removes no member.
- A partial execution reports exact member/path state and does not overstate
  freed bytes.
- Orphaned cleanup remains functional.
- `--force` does not override locked rows or force Git removal.

### UX And Compatibility

- Policy header independently shows idle age, recent window, retention count,
  and size threshold.
- Locked, retained, and recommended reasons are deterministic in TTY and text
  modes.
- Protected-only high-pressure Codex states enter guided review by default.
- Explicit classic selectors and `--no-guide` remain classic.
- `scan --json` compatibility tests pass.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass.

### Dogfood Success Bar

On the preserved 2026-07-10 evidence shape or an equivalent sanitized fixture:

- upstream absence alone accounts for zero locked rows
- every dirty unit remains locked
- every unreferenced detached HEAD remains locked
- every referenced detached HEAD proceeds to soft policy
- multi-member units are inspected rather than reported generically unavailable
- recommended reclaimable space is at least 10 GB on the current-machine
  baseline while hard-locked space remains protected
- changing idle age affects recommendations when age-eligible safe rows exist

## Test Plan

### Unit Tests

- cleanup-unit grouping: direct, nested, duplicate, and multi-member targets
- canonical repository identity and same-basename separation
- Git status parsing and dirty aggregation
- named branch without upstream
- gone upstream
- detached HEAD reachable by local ref
- detached HEAD reachable by remote ref
- detached HEAD unreachable by any ref
- activity-source precedence and unit max activity
- recent-window lock independent of idle age
- top-3 ranking, ties, and multi-repository retention
- policy precedence and stable reason codes
- age replanning with preserved user overrides
- selected/protected normalized totals

### Command Tests

- default clean enters guided review for protected-only pressure
- explicit classic selectors stay classic
- TTY and non-TTY show the same policy class and reason
- locked rows cannot be toggled or forced
- dry-run never executes Git removal
- confirmation path remains after preview

### Executor Integration Tests

Create temporary repositories and worktrees covering:

- attached local-only branch without upstream
- attached branch with gone upstream
- detached but referenced HEAD
- detached unreferenced HEAD
- dirty/untracked worktree
- two worktrees under one cleanup container
- Git-aware removal failure

Assert member paths, `git worktree list`, branch refs, OIDs, receipts, and freed
size accounting after each run.

### Manual Dogfood

```bash
aibris clean --dry-run
aibris clean --dry-run --age 3d
aibris clean --dry-run --no-guide
printf '\n' | aibris clean --dry-run --age 3d
```

Before any real removal, capture:

- plan class counts and sizes
- reason distribution
- canonical repository grouping
- selected branch refs and OIDs

After a controlled confirmed removal, verify branch preservation and parent
worktree metadata cleanup.

## Success Metrics

### Safety

- Zero dirty/untracked units default-selected.
- Zero unreferenced detached HEADs selectable.
- Zero branch refs deleted by active worktree cleanup.
- Zero raw recursive fallbacks after Git-aware active removal failure.
- Receipts never overstate bytes for partially removed units.

### Utility

- Missing upstream alone never locks a row.
- Age controls change idle policy rather than appearing inert behind upstream
  locks.
- Current dogfood baseline recommends at least 10 GB while preserving hard
  locks.
- Project retention uses canonical repositories, not fragmented display names.

### Explainability

- Every row has one primary decision class and stable ordered reasons.
- Every locked row names the unsafe or unavailable evidence.
- Every recommended local-only branch states that its branch ref is retained.

## Rollout

### Phase 1: Evidence And Cleanup Units

Land the internal cleanup-unit/member model, canonical repository identity,
activity evidence, and reachability inspection behind tests. Do not change
recommendations or deletion behavior yet.

### Phase 2: Git-Aware Executor

Add preflight, active Git removal, branch-preservation checks, multi-member
handling, and truthful receipts. Keep the old planner policy until executor
safety is established.

### Phase 3: Policy Cutover

Enable the hierarchical policy, new reason codes, separate age semantics, and
protected-only guided routing. Refresh command tests and sanitized fixtures.

### Phase 4: Dogfood And Release

Run dry-run comparison on the current-machine evidence shape, perform one
controlled confirmed removal, update docs, and release as `v0.8.0` after CI and
installer smoke pass.

No hidden feature flag is required if each phase preserves prior behavior until
the final policy cutover. If Phase 2 cannot safely remove active multi-member
units, keep those units locked and release the remaining policy only after an
explicit scope decision.

## Risks And Mitigations

### Risk: Local-Only Commits Feel Unsafe To Remove

The local branch ref remains and is verified before and after removal. Explain
this in the row reason and preview. Never delete branch refs in this milestone.

### Risk: Ref Reachability Is Misinterpreted

Use Git's own ref enumeration and commit containment semantics. Cover local,
remote, attached, detached, and same-OID cases with temporary repository tests.

### Risk: Reflog Activity Is Missing Or Expired

Treat reflog as one activity source, not the sole safety source. Dirty status,
session metadata, retention, and idle age remain independent protections.

### Risk: Multi-Member Removal Partially Succeeds

Preflight all members, remove through Git, stop on first execution failure, and
report exact state. Never credit the whole target until the physical container
is gone.

### Risk: Three Retained Units Is Arbitrary

It is a conservative product default backed by current usage shape, not a hard
safety rule. Keep it injectable in tests and reviewable in the checklist. Do
not add a public flag until dogfood shows a real need.

### Risk: Six Hours Is Too Short For Paused Work

Recent-three retention and minimum idle age cover paused work beyond the hard
window. The 6-hour window protects immediate liveness; it is not the only
retention mechanism.

### Risk: Default Guided Review Appears With Nothing Selectable

That is intentional when Codex worktree pressure is material. Show zero
selected plus precise locked reasons; Enter remains a no-op and `--no-guide`
remains the escape hatch.

### Risk: Public JSON Consumers Expect New Fields

Keep the richer cleanup-unit model internal for v0.8.0. A future schema revision
may expose it deliberately with versioning.

## Registered Milestone And Epic Decomposition

### Milestone

[#5 Evidence-Based Worktree Reclamation](https://github.com/sungjunlee/aibris/milestone/5)

Goal: reclaim substantial stale Codex worktree space while hard-locking live,
dirty, unreadable, and unrecoverable state, then ship the policy and Git-aware
executor as `v0.8.0`.

Backlog prerequisite completed on 2026-07-12: the prior `Default Guided Clean`
sprint and GitHub milestone #4 were closed before this milestone's active sprint
was initialized.

### Epic A: [#80 Build Recoverable Cleanup Units](https://github.com/sungjunlee/aibris/issues/80)

Owns physical identity, Git evidence, and safe execution.

### Epic B: [#81 Make Guided Retention Policy Useful](https://github.com/sungjunlee/aibris/issues/81)

Owns activity, retention, recommendation policy, routing, output, and rollout.

## Recommended Issue Split

The registered issue numbers and order below are the source of truth for the
active milestone sprint.

| Order | Epic | Issue | Scope | Depends on | Done signal |
| ---: | --- | --- | --- | --- | --- |
| 1 | A | [#82 Model physical cleanup units and nested Git members](https://github.com/sungjunlee/aibris/issues/82) | Build deterministic one-target/many-member internal model without changing public scan JSON | none | Direct, nested, duplicate, and two-member fixtures pass |
| 2 | A | [#83 Resolve canonical repository identity](https://github.com/sungjunlee/aibris/issues/83) | Add common-dir identity, same-basename separation, and multi-repository unit membership | #82 | Retention input groups by canonical repository |
| 3 | A | [#84 Replace upstream safety with ref reachability](https://github.com/sungjunlee/aibris/issues/84) | Collect branch, HEAD, containing refs, dirty state, and stable reasons | #82 | Named no-upstream and referenced detached states pass; unreferenced detached locks |
| 4 | B | [#85 Add unified worktree activity evidence](https://github.com/sungjunlee/aibris/issues/85) | Combine session metadata, HEAD reflog, and fallback timestamps without reading bodies | #82 | Per-member and per-unit last activity tests pass |
| 5 | A | [#86 Add Git-aware active worktree executor](https://github.com/sungjunlee/aibris/issues/86) | Preflight, Git removal, branch preservation, multi-member handling, truthful receipts | #83, #84 | Integration tests prove refs preserved and no raw fallback |
| 6 | B | [#87 Implement hierarchical retention and recommendation policy](https://github.com/sungjunlee/aibris/issues/87) | Separate 6h lock, top-3 retention, 3d idle age, 256 MB size, and remove no-low-risk indefinite protection | #83, #84, #85 | Policy matrix and deterministic reason tests pass |
| 7 | B | [#88 Reclassify guided rows and orthogonalize age controls](https://github.com/sungjunlee/aibris/issues/88) | Map locked/retained/recommended correctly and make age affect idle only | #87 | TTY/text parity and age-replan tests pass |
| 8 | B | [#89 Route protected-only Codex pressure to guided review](https://github.com/sungjunlee/aibris/issues/89) | Use pressure rather than selected count while preserving classic selectors | #87 | Default/classic/non-TTY command tests pass |
| 9 | A + B | [#90 Dogfood Git-aware reclamation and refresh docs](https://github.com/sungjunlee/aibris/issues/90) | Capture before/after evidence and one controlled branch-preserving removal | #86, #88, #89 | DOGFOOD, SPEC, README, skill, and evidence updated |
| 10 | A + B | [#91 Release v0.8.0](https://github.com/sungjunlee/aibris/issues/91) | Version, changelog, snapshot, CI, tag, GitHub Release, installer smoke | #90 | Published release and installer report v0.8.0 |

## Suggested Delivery Batches

### Batch 1: Identity Foundation

- Issue 1: cleanup units and members
- Issue 2: canonical repository identity

Issue 2 follows Issue 1 because repository identity belongs to members rather
than scanner rows.

### Batch 2: Independent Evidence

- Issue 3: Git recoverability
- Issue 4: activity evidence

These may proceed in parallel after the cleanup-unit interface is stable.

### Batch 3: Execution Safety

- Issue 5: Git-aware executor

This must land before the planner starts recommending states that the old raw
executor was designed to avoid.

### Batch 4: Policy Cutover

- Issue 6: hierarchical policy
- Issue 7: guided classification and age controls
- Issue 8: protected-only routing

Issue 6 is the review anchor. Issues 7 and 8 should follow its stable reason and
decision interfaces rather than independently reimplement policy.

### Batch 5: Evidence And Release

- Issue 9: dogfood and documentation
- Issue 10: v0.8.0 release

## Milestone Definition Of Done

- Both epics and every non-deferred child issue are closed by merged work.
- Cleanup units preserve all nested Git members through planning and execution.
- Missing upstream alone never produces a locked decision.
- Dirty, unreadable, recent, and unreferenced detached state remains locked.
- Active removal preserves branch refs and cleans parent worktree metadata.
- Recent-three retention uses canonical repository identity.
- Age controls affect idle eligibility only.
- Protected-only pressure enters guided review with clear reasons.
- Current-machine dogfood meets the safety and utility success bars.
- README, SPEC, DOGFOOD, skill workflow, and changelog match shipped behavior.
- `go test ./...`, `go build ./...`, `go vet ./...`, release snapshot, CI, GitHub
  Release, and installer smoke all pass for `v0.8.0`.

## Open Questions

These do not block drafting issues unless implementation evidence changes the
recommended default.

1. Should an activity-index outage remain locked for all automatic
   recommendations in v0.8.0, or may strong Git and reflog evidence downgrade
   it to reviewable?
2. Should a multi-member unit spanning repositories display every repository in
   the row or a compact count with details in the footer?
3. Should successful active removal call targeted metadata prune in addition to
   `git worktree remove`, or is postcondition verification sufficient?
4. Should `KeepPerRepository` become user-configurable after dogfood, or remain
   a product default with manual checklist override?
5. Should the human label change from `active` to `linked` in v0.8.0 while
   preserving the existing JSON status for compatibility?

## Final Recommendation

Proceed with the milestone and issue order above. Do not ship a narrow patch
that merely stops locking missing upstream. The policy becomes safe and useful
only when cleanup-unit identity, ref reachability, recent activity, per-repo
retention, and Git-aware execution land as one coherent milestone.
