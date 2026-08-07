---
id: AIB-116
title: Dogfood the unified cleanup journey on representative homes
status: Done
labels:
  - documentation
  - cli
  - scanner
  - safety
  - type:chore
priority: medium
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
completed_date: '2026-08-08'
---
## Description
## Goal

Validate the unified journey against realistic mixed debris without deleting valuable local state.

## Acceptance criteria

- [x] Fixtures cover caches, node_modules, orphaned worktrees, safe active
      units, and hard-locked units together
      (`cmd/dogfood_unified_cleanup_test.go`, run in CI).
- [x] A sanitized real-home scan and dry-run records time, found bytes,
      eligible bytes, and protected bytes (see `docs/DOGFOOD.md`).
- [x] No real deletion is performed without a separately approved disposable
      fixture; the real-home run ended `[DRY-RUN] No files were removed.`
- [x] The default next command leads to a useful plan when guided selection
      is empty: guided selected 0, yet the unified review selected 9 classic
      candidates (orphaned agent-state, npm cache, node_modules).
- [x] Documentation examples are generated or checked against actual output
      (DOGFOOD evidence section added; classic CLI examples verified against
      the unchanged classic contract).
