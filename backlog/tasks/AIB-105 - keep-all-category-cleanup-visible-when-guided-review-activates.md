---
id: AIB-105
title: Keep all-category cleanup visible when guided review activates
status: To Do
labels:
  - enhancement
  - ux
  - cli
  - safety
  - type:bug
priority: critical
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
---
## Description
## Problem

A plain clean --dry-run can enter guided Codex review and return before showing eligible node_modules and cache candidates. A real dogfood scan found 21.3 GB; guided review selected 0 B while classic review still had 2.8 GB eligible.

## Desired behavior

Guided worktree decisions must not hide safe candidates from other categories. The default flow should produce one coherent audit, or continue to classic candidates when guided selection is empty.

## Acceptance criteria

- [ ] A no-selector dry-run reports eligible non-worktree candidates even when guided Codex pressure exists.
- [ ] Hard-locked worktrees remain locked and cannot be selected.
- [ ] Guided and classic target normalization does not double-count overlapping paths.
- [ ] Non-TTY behavior is deterministic and documented.
- [ ] Regression tests cover guided-selected, guided-empty, and mixed-category cases.
- [ ] go test ./..., go build ./..., and go vet ./... pass.
