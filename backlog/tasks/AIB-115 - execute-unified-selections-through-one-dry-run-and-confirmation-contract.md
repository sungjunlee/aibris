---
id: AIB-115
title: Execute unified selections through one dry-run and confirmation contract
status: To Do
labels:
  - cli
  - safety
  - type:feature
priority: high
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
---
## Description
## Goal

Ensure every selected target crosses the same preview, preflight, confirmation, execution, and receipt boundaries regardless of category.

## Acceptance criteria

- [ ] Dry-run and real execution differ only by the explicit execution gate.
- [ ] Approved selectors and safety flags remain identical between preview and execution.
- [ ] Active worktree preflight is refreshed immediately before mutation.
- [ ] Partial failure reports accurate freed bytes and returns non-zero.
- [ ] --force skips only the final confirmation and never hard safety.
- [ ] Classic compatibility flags remain documented and tested.
