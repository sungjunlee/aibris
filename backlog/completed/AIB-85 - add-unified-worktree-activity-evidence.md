---
id: AIB-85
title: Add unified worktree activity evidence
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

- Epic: #81
- Milestone: Evidence-Based Worktree Reclamation
- PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md

## Dependencies

#82

## Scope

- Reuse metadata-only Codex session activity.
- Add per-worktree HEAD reflog activity.
- Compute member and cleanup-unit last activity with documented precedence.
- Do not read conversation bodies or recursively walk file mtimes.

## Acceptance Criteria

- [x] Session, reflog, and fallback precedence is deterministic.
- [x] A multi-member unit uses the newest member activity.
- [x] Activity-index outage fails closed for automatic recommendation.
- [x] Context cancellation and cache behavior are tested.
