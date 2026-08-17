---
id: AIB-128
title: Add explicit exclusions and project ignore configuration
status: To Do
labels:
  - enhancement
  - cli
  - scanner
  - safety
  - type:feature
priority: medium
milestone: Future
created_date: '2026-07-22'
---
## Description
## Goal

Let users exclude private, slow, or intentionally retained trees without allowing arbitrary cleanup targets.

## Acceptance criteria

- [ ] Repeatable --exclude paths or patterns are scoped under approved scan roots.
- [ ] A documented per-user ignore file can express persistent exclusions.
- [ ] Exclusions affect discovery only and cannot broaden deletion authority.
- [ ] Human and JSON output explain excluded scope when diagnostics are enabled.
- [ ] Symlink and path traversal cases are tested.
- [ ] Defaults remain unchanged for users without configuration.
