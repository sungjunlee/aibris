---
id: AIB-80
title: '[Epic] Build recoverable worktree cleanup units'
status: Done
labels:
  - enhancement
  - scanner
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
completed_date: '2026-07-13'
---
## Description
## Context

The current guided planner locks most reclaimable worktree space because linked health and missing upstream are used as proxies for liveness and recoverability.

PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md
Planning PR: https://github.com/sungjunlee/aibris/pull/79

## Scope

- physical cleanup-unit and nested Git-member identity
- canonical repository identity
- ref-reachability safety evidence
- Git-aware active worktree execution
- branch-preservation and truthful cleanup receipts

## Done Criteria

- [x] Every child issue is closed by merged work.
- [x] Active worktree removal preserves branch refs and cleans parent worktree metadata.
- [x] Dirty, unreadable, and unreferenced detached state remains locked.
- [x] `go test ./...`, `go build ./...`, and `go vet ./...` pass.

## Child Issues

- [x] #82 Model physical cleanup units and nested Git members
- [x] #83 Resolve canonical repository identity for cleanup units
- [x] #84 Replace upstream safety with ref reachability
- [x] #86 Add Git-aware active worktree executor

## Completion Evidence

- PRs #92, #93, #94, and #97 merged; issues #82, #83, #84, and #86 closed.
- Controlled removal preserved branch refs and parent worktree metadata.
- v0.8.0 validation and release workflow passed. Local validation ran
  `go test ./...`, `go build ./...`, and `go vet ./...`; PR #102 CI run
  `29201757156` passed on Ubuntu and macOS.
