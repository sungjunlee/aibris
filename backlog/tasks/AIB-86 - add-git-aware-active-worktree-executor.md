---
id: AIB-86
title: Add Git-aware active worktree executor
status: To Do
labels:
  - enhancement
  - cli
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
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

- [ ] Attached local-only branch refs and OIDs survive removal.
- [ ] Referenced detached commits remain reachable.
- [ ] Git-aware failure never falls back to raw active-path deletion.
- [ ] Failed multi-member preflight removes nothing.
- [ ] Receipts do not overstate bytes for partially removed units.
- [ ] CLI --force does not become Git removal force.
