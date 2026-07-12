---
milestone: Evidence-Based Worktree Reclamation
status: completed
started: 2026-07-12
due: 2026-07-26
objectives: []
component: ""
---

# Evidence-Based Worktree Reclamation

## Goal

Ship v0.8.0 with evidence-based active worktree reclamation that recommends
meaningful stale space while hard-locking live, dirty, unreadable, and
unrecoverable state and preserving branch refs during Git-aware removal.

## Source Of Truth

- GitHub milestone: https://github.com/sungjunlee/aibris/milestone/5
- Epic A: #80 `[Epic] Build recoverable worktree cleanup units`
- Epic B: #81 `[Epic] Make guided reclamation policy useful`
- PRD:
  `docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md`
- Planning PR: #79 `Define evidence-based worktree reclamation`

## Plan

### Batch 1 - Cleanup Unit Foundation

- [x] #82 Model physical cleanup units and nested Git members (~90min)

### Batch 2 - Independent Evidence

- [x] #83 Resolve canonical repository identity for cleanup units (~60min)
- [x] #84 Replace upstream safety with ref reachability (~90min)
- [x] #85 Add unified worktree activity evidence (~90min)

### Batch 3 - Git-Aware Execution

- [x] #86 Add Git-aware active worktree executor (~120min)

### Batch 4 - Hierarchical Policy

- [x] #87 Implement hierarchical retention and recommendation policy (~120min)

### Batch 5 - Guided Product Cutover

- [x] #88 Reclassify guided rows and orthogonalize age controls (~75min)
- [x] #89 Route protected-only Codex pressure to guided review (~60min)

### Batch 6 - Dogfood And Documentation

- [x] #90 Dogfood Git-aware reclamation and refresh documentation (~120min)

### Batch 7 - Release

- [x] #91 Release v0.8.0 evidence-based worktree reclamation (~60min)

## Definition Of Done

- GitHub issues #82-#91 are closed by merged work or explicitly deferred with
  milestone and epic comments.
- Missing upstream alone never locks a cleanup unit.
- Dirty, unreadable, recently active, and unreferenced detached state remains
  locked.
- Canonical repository identity drives recent-three retention.
- Active removal preserves branch refs and cleans parent worktree metadata.
- Multi-member preflight and partial-failure receipts report exact state.
- Protected-only Codex pressure enters guided review with deterministic reasons.
- Current-machine dogfood recommends at least 10 GB while preserving hard
  locks.
- README, SPEC, DOGFOOD, skill workflow, and changelog match shipped behavior.
- `go test ./...`, `go build ./...`, `go vet ./...`, GoReleaser snapshot, CI,
  GitHub Release, and installer smoke pass for v0.8.0.

## Running Context

- PR #79 is the review anchor for the policy and issue graph. Implementation
  should start from `origin/main` after that PR lands, not from this planning
  branch.
- The v0.7.0 checklist and recommended/reviewable/locked selection model stay in
  place. This milestone changes evidence, policy, routing, and active removal.
- The preserved 2026-07-10 baseline was 39 units / 33.9 GB: 3 recommended /
  3.1 GB, 2 reviewable / 0.1 GB, and 34 locked / 30.7 GB. Upstream comparison
  accounted for 32 locked rows / 28.2 GB.
- A cleanup unit is one physical target with one or more Git members. All
  members must pass hard safety; size and deletion are counted once per unit.
- Canonical Git common-dir is the internal repository identity. Path-derived
  project names remain display-only.
- Missing or gone upstream is explanatory metadata. Recoverability comes from
  an attached local branch or detached HEAD reachability from named refs.
- `RecentActivityWindow=6h`, `KeepPerRepository=3`, `MinIdleAge=3d`, and
  `MinSize=256MB` are independent policy inputs. Explicit `--age` changes only
  minimum idle age.
- CLI `--force` skips only final confirmation. It never unlocks hard-safety rows
  or becomes `git worktree remove --force`.
- The Git-aware executor must land before policy cutover so newly recommended
  active worktrees are not handed to the old raw deletion path.
- `scan --json` remains compatible; richer cleanup-unit evidence is internal to
  the guided command path for v0.8.0.

## Progress

- 2026-07-12: Closed the completed Default Guided Clean sprint and GitHub
  milestone #4, then registered milestone #5, epics #80/#81, and leaf issues
  #82-#91 from the evidence-based reclamation PRD.
- 2026-07-12: Pulled all 12 GitHub issues into local task mirrors and created
  this dependency-ordered active sprint. Planning PR #79 remains the merge gate
  before implementation dispatch.
- 2026-07-13: Issues #82-#91 closed through merged PRs #92-#101. Independent
  Claude and Codex review rounds caught dead code, state aliasing, receipt-doc
  drift, and selector-scope leakage before merge.
- 2026-07-13: Release workflow `29201631207` published v0.8.0 with six platform
  archives plus checksums; tagged installer smoke reported
  `aibris version 0.8.0` from a disposable prefix.
