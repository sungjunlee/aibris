---
id: AIB-90
title: Dogfood Git-aware reclamation and refresh documentation
status: Done
labels:
  - documentation
  - docs
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

#86, #88, #89

## Scope

- Capture before/after reason distribution and canonical repository grouping.
- Perform one controlled branch-preserving active removal.
- Update README, SPEC, DOGFOOD, skill workflow, and checklist policy docs.

## Acceptance Criteria

- [x] Missing upstream accounts for zero locked rows in dogfood.
- [x] Dirty and unreferenced detached states remain locked.
- [x] Multi-member units are inspected rather than generically unavailable.
- [x] At least 10 GB is recommended on the preserved baseline shape.
- [x] Branch refs and parent worktree metadata are verified after controlled removal.
- [x] Docs match shipped command behavior.
