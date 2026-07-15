---
id: AIB-87
title: Implement hierarchical retention and recommendation policy
status: Done
labels:
  - enhancement
  - ux
  - cli
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
completed_date: '2026-07-13'
---
## Description
## Parent

- Epic: #81
- Milestone: Evidence-Based Worktree Reclamation
- PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md

## Dependencies

#83, #84, #85

## Scope

- Separate hard locks, per-repository retention, idle age, and size threshold.
- Default to recent<6h locked, keep=3/repo, idle>3d, and min-size=256MB.
- Remove indefinite no-low-risk-session protection.
- Produce stable ordered decision reasons.

## Acceptance Criteria

- [x] Activity within 6 hours is locked regardless of --age.
- [x] The three most recent units per canonical repository are reviewable.
- [x] The fourth safe old large unit becomes recommended.
- [x] Historical session existence alone does not protect forever.
- [x] A multi-repository unit is retained when top-3 in any member repository.
