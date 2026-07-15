---
id: AIB-84
title: Replace upstream safety with ref reachability
status: Done
labels:
  - enhancement
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
completed_date: '2026-07-13'
---
## Description
## Parent

- Epic: #80
- Milestone: Evidence-Based Worktree Reclamation
- PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md

## Dependencies

#82

## Scope

- Collect dirty state, branch ref, HEAD OID, containing local refs, and containing remote refs.
- Treat upstream presence as explanatory metadata only.
- Lock detached HEAD only when no named ref reaches it.

## Acceptance Criteria

- [x] A clean named branch without upstream is not locked.
- [x] A gone upstream does not independently lock a row.
- [x] Referenced detached HEAD proceeds to soft policy.
- [x] Unreferenced detached HEAD and dirty/untracked state remain locked.
- [x] Stable reason codes cover every evidence result.
