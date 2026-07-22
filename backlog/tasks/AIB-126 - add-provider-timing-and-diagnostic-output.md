---
id: AIB-126
title: Add provider timing and diagnostic output
status: To Do
labels:
  - documentation
  - enhancement
  - ux
  - cli
  - scanner
  - type:feature
  - area:automation
priority: medium
milestone: '0.x Automation & Schema'
created_date: '2026-07-22'
---
## Description
## Goal

Make slow or incomplete scans diagnosable without requiring source-level debugging.

## Acceptance criteria

- [ ] An opt-in diagnostics mode reports provider duration, item count, bytes, and error state.
- [ ] Normal output remains concise.
- [ ] JSON diagnostics have a documented stable shape or are clearly marked experimental.
- [ ] Diagnostics do not expose AI conversation bodies or unrelated file contents.
- [ ] Cancellation and permission failures identify the responsible provider.
- [ ] Tests use deterministic clocks rather than timing-sensitive sleeps.
