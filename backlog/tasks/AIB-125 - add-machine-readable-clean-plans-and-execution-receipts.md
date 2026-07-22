---
id: AIB-125
title: Add machine-readable clean plans and execution receipts
status: To Do
labels:
  - cli
  - safety
  - type:feature
  - area:automation
priority: high
milestone: '0.x Automation & Schema'
created_date: '2026-07-22'
---
## Description
## Goal

Let agents and scripts consume the same cleanup decisions, safety reasons, and final outcomes that humans see.

## Acceptance criteria

- [ ] Dry-run can emit a versioned JSON plan without performing deletion.
- [ ] Real execution emits or stores a versioned receipt with requested, removed, failed, protected, and freed-byte totals.
- [ ] Physical targets and visible logical rows are not double-counted.
- [ ] Sensitive paths are emitted only when explicitly requested by the command output contract.
- [ ] Exit status and receipt status agree for success, cancellation, and partial failure.
- [ ] Human output remains the default.
