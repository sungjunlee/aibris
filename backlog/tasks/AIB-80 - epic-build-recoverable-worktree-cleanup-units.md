---
id: AIB-80
title: '[Epic] Build recoverable worktree cleanup units'
status: To Do
labels:
  - enhancement
  - scanner
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
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

- [ ] Every child issue is closed by merged work.
- [ ] Active worktree removal preserves branch refs and cleans parent worktree metadata.
- [ ] Dirty, unreadable, and unreferenced detached state remains locked.
- [ ] `go test ./...`, `go build ./...`, and `go vet ./...` pass.

## Child Issues

- [ ] #82 Model physical cleanup units and nested Git members
- [ ] #83 Resolve canonical repository identity for cleanup units
- [ ] #84 Replace upstream safety with ref reachability
- [ ] #86 Add Git-aware active worktree executor
