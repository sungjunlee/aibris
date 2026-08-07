---
id: AIB-115
title: Execute unified selections through one dry-run and confirmation contract
status: Done
labels:
  - cli
  - safety
  - type:feature
priority: high
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
completed_date: '2026-08-07'
---
## Description
## Goal

Ensure every selected target crosses the same preview, preflight, confirmation, execution, and receipt boundaries regardless of category.

## Acceptance criteria

- [x] Dry-run and real execution differ only by the explicit execution gate:
      the unified plan renders once and execution adds only validation,
      confirmation, and the executor.
- [x] Approved selectors and safety flags remain identical between preview and
      execution (classic route unchanged; guided route reuses the same flags).
- [x] Active worktree preflight is refreshed immediately before mutation
      (existing Git-aware executor contract unchanged).
- [x] Partial failure reports accurate freed bytes and returns non-zero
      (existing receipt contract unchanged).
- [x] --force skips only the final confirmation and never hard safety.
- [x] Classic compatibility flags remain documented and tested (`--no-guide`,
      explicit selectors keep the classic audit/executor contract).
