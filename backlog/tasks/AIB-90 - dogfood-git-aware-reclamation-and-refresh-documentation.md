---
id: AIB-90
title: Dogfood Git-aware reclamation and refresh documentation
status: To Do
labels:
  - documentation
  - docs
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
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

- [ ] Missing upstream accounts for zero locked rows in dogfood.
- [ ] Dirty and unreferenced detached states remain locked.
- [ ] Multi-member units are inspected rather than generically unavailable.
- [ ] At least 10 GB is recommended on the preserved baseline shape.
- [ ] Branch refs and parent worktree metadata are verified after controlled removal.
- [ ] Docs match shipped command behavior.
