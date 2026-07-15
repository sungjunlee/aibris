---
id: AIB-83
title: Resolve canonical repository identity for cleanup units
status: Done
labels:
  - enhancement
  - scanner
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

- Resolve each member's canonical Git common-dir.
- Separate internal repository identity from display basename.
- Support cleanup units spanning multiple repositories.

## Acceptance Criteria

- [x] Differently named worktrees from one repository share an identity.
- [x] Same-basename repositories remain distinct.
- [x] Multi-repository units expose every member repository deterministically.
- [x] Retention inputs no longer depend on path-derived project labels.
