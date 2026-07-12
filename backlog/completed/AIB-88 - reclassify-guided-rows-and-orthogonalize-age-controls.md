---
id: AIB-88
title: Reclassify guided rows and orthogonalize age controls
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

#87

## Scope

- Map hard failures to locked, retention/age/size holds to reviewable, and eligible units to recommended.
- Make --age and checklist age commands change only minimum idle age.
- Show all independent policy values in TTY and text output.

## Acceptance Criteria

- [x] Recent activity is locked, not reviewable.
- [x] Upstream absence never maps directly to locked.
- [x] Age replanning cannot unlock recent or unrecoverable state.
- [x] User overrides survive replanning when rows remain selectable.
- [x] TTY and text modes show identical class and reason.
