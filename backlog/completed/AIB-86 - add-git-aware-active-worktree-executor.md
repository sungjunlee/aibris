---
id: AIB-86
title: Add Git-aware active worktree executor
status: Done
labels:
  - enhancement
  - cli
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

#83, #84

## Scope

- Preflight selected active units immediately before mutation.
- Remove active members with Git worktree semantics.
- Preserve branch refs and verify postconditions.
- Handle multi-member partial failures and truthful receipts.
- Keep orphaned path cleanup stable.

## Acceptance Criteria

- [x] Attached local-only branch refs and OIDs survive removal.
- [x] Referenced detached commits remain reachable.
- [x] Git-aware failure never falls back to raw active-path deletion.
- [x] Failed multi-member preflight removes nothing.
- [x] Receipts do not overstate bytes for partially removed units.
- [x] CLI --force does not become Git removal force.
