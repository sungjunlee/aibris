---
id: AIB-114
title: Render mixed-category cleanup review from one plan
status: Done
labels:
  - enhancement
  - ux
  - cli
  - safety
  - type:feature
priority: high
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
---
## Description
## Goal

Show cache, dependency, orphaned worktree, and guided active-worktree decisions together while keeping the default path readable.

## Acceptance criteria

- [x] The audit shows found, eligible, selected, reviewable, and protected totals without double counting.
- [x] Guided worktree reasons remain visible without obscuring other categories.
- [x] TTY interaction and text fallback use the same plan state.
- [x] A user can accept defaults, toggle selectable rows, or abort.
- [x] Empty sections are omitted without hiding a non-empty section.
- [x] Snapshot or golden tests cover narrow and wide terminals where applicable.
