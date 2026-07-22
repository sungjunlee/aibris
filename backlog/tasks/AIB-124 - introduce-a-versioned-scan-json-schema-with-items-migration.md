---
id: AIB-124
title: Introduce a versioned scan JSON schema with items migration
status: To Do
labels:
  - enhancement
  - cli
  - scanner
  - safety
  - type:feature
  - area:automation
priority: high
milestone: '0.x Automation & Schema'
created_date: '2026-07-22'
---
## Description
## Goal

Evolve the historical worktrees array into a truthful all-debris contract without abruptly breaking existing consumers.

## Acceptance criteria

- [ ] Output includes an explicit schema_version.
- [ ] A canonical items array represents every debris category.
- [ ] The historical worktrees field is retained as a documented 0.x compatibility alias for a defined period.
- [ ] Complete and partial scan semantics from #108 are represented.
- [ ] Field types, enum values, and compatibility rules are documented with fixtures.
- [ ] Contract tests compare exact JSON shapes across representative categories.
