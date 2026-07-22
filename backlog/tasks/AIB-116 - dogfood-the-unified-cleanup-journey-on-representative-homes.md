---
id: AIB-116
title: Dogfood the unified cleanup journey on representative homes
status: To Do
labels:
  - documentation
  - cli
  - scanner
  - safety
  - type:chore
priority: medium
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
---
## Description
## Goal

Validate the unified journey against realistic mixed debris without deleting valuable local state.

## Acceptance criteria

- [ ] Fixtures cover caches, node_modules, orphaned worktrees, safe active units, and hard-locked units together.
- [ ] A sanitized real-home scan and dry-run records time, found bytes, eligible bytes, and protected bytes.
- [ ] No real deletion is performed without a separately approved disposable fixture.
- [ ] The default next command leads to a useful plan when guided selection is empty.
- [ ] Documentation examples are generated or checked against actual output.
