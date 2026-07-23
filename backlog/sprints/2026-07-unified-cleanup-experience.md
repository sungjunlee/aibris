---
milestone: 0.9.x Unified Cleanup Experience
status: active
started: 2026-07-23
due: TBD
objectives: []
component: ""
---

# 0.9.x Unified Cleanup Experience

## Goal

Scan, review, dry-run, confirmation, execution, and receipts operate on one
all-category plan while preserving every hard safety lock and explicit
selector.

## Plan

### Batch 1 — Establish one source of cleanup truth

- [ ] #113 Design the unified cleanup plan model (~2h)

### Batch 2 — Review every category together

- [ ] #114 Render mixed-category cleanup review from one plan (~3h)

### Batch 3 — Cross one execution boundary

- [ ] #115 Execute unified selections through one dry-run and confirmation contract (~3h)

### Batch 4 — Prove the journey with representative evidence

- [ ] #116 Dogfood the unified cleanup journey on representative homes (~2h)

### Batch 5 — Close capability scope without forcing a release

- [ ] #112 [Epic] Build one coherent cleanup journey (~30min)

## Deferred Release Gate

- #117 Prepare a 0.9.x unified-cleanup release remains an explicit
  maintainer-approval gate. Add it to execution only after the experience is
  accepted; otherwise keep it open without a tag or date.

## Running Context

- GitHub Issues remain the task source of truth; this file owns execution
  order and cross-task decisions.
- The guided-then-classic handoff shipped in 0.8.x is an interim compatibility
  bridge. Replace it incrementally without weakening `--no-guide`, explicit
  selectors, or non-TTY behavior.
- Hard locks dominate every selection. A selected parent must never cover a
  locked descendant, and `--force` may skip only final confirmation.
- The plan must keep visible policy rows distinct from normalized physical
  targets so totals and receipts never double count nested or duplicate paths.
- Partial scans cannot authorize cleanup. Cancellation and stale evidence must
  be explicit model states rather than renderer-specific behavior.
- The project remains in 0.x. Milestone #7 has no due date, and #117 cannot be
  completed without explicit maintainer approval.

## Progress

- 2026-07-23: Activated milestone #7 after cleanly closing the 0.8.x
  implementation sprint. Reassessment confirmed #113 as the dependency for
  renderer and execution work; release gate #117 stays outside active
  execution pending maintainer approval.
- 2026-07-23: Implemented #113 on `codex/unified-cleanup-plan`. The new
  renderer-independent model separates visible rows from exact physical
  targets, propagates descendant hard locks, normalizes overlap-safe totals,
  adapts classic and worktree policies, and validates cancellation, partial
  scans, and stale evidence. Race tests, build, and vet pass locally.
