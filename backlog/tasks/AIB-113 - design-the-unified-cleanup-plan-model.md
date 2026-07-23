---
id: AIB-113
title: Design the unified cleanup plan model
status: In Progress
labels:
  - enhancement
  - cli
  - scanner
  - safety
  - type:feature
priority: high
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
---
## Description
## Goal

Define one internal plan that can represent eligible classic targets, recommended and reviewable worktree units, locked targets, reasons, normalized physical size, and execution intent.

## Acceptance criteria

- [x] The model distinguishes physical targets from visible rows.
- [x] It represents selectable, unselected, and hard-locked decisions.
- [x] Size accounting is deterministic and overlap-safe.
- [x] Classic and guided policies can populate the same plan without policy duplication.
- [x] The design includes cancellation, stale evidence, and partial-scan behavior.
- [x] Focused unit tests cover mixed-category plans.
