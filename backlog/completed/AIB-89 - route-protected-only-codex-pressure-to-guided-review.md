---
id: AIB-89
title: Route protected-only Codex pressure to guided review
status: Done
labels:
  - enhancement
  - ux
  - cli
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

- Choose guided review from active Codex cleanup pressure, not selected count alone.
- Open guided review for at least 256 MB or three validated active units.
- Preserve explicit classic selectors, --no-guide, and non-TTY behavior.

## Acceptance Criteria

- [x] Protected-only high-pressure state opens guided review with zero selected.
- [x] Enter with zero selected performs no preview or deletion.
- [x] Explicit classic selectors and --no-guide remain classic.
- [x] Non-TTY input never hangs.
- [x] --dry-run remains delete-free.
