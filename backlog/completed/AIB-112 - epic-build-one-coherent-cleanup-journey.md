---
id: AIB-112
title: '[Epic] Build one coherent cleanup journey'
status: Done
labels:
  - enhancement
  - ux
  - cli
  - scanner
  - safety
  - type:feature
priority: high
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
completed_date: '2026-08-08'
---
## Description
## Outcome

Turn scan -> review -> dry-run -> clean into one all-category journey. Guided worktree judgment remains a specialized section inside the journey, not an exclusive mode that hides other debris.

## Completion criteria

- [x] The unified plan has one safety and accounting model (#113 + #115/#192).
- [x] Human and non-TTY flows expose the same candidate set and decisions.
- [x] Existing explicit selectors remain compatible.
- [x] Real-home dogfood demonstrates useful reclamation without weakening
      locks (#116, docs/DOGFOOD.md).
- [x] The project remains explicitly pre-1.0.

## Completion evidence

- #113 (PR #135), #114 (PR #136), #115 (PR #192), #116 (PR #193) merged.
- #117 remains the explicit maintainer-approval release gate (intentionally
  open; epic close implies no release commitment).
