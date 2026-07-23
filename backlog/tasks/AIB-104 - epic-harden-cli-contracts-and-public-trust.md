---
id: AIB-104
title: '[Epic] Harden CLI contracts and public trust'
status: To Do
labels:
  - documentation
  - enhancement
  - ux
  - area:oss
  - cli
  - safety
  - type:chore
priority: critical
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
---
## Description
## Outcome

Make aibris predictable for humans, scripts, and AI agents before expanding its feature surface.

## Scope

- Keep all cleanup candidates visible when guided review activates.
- Reject invalid selector values before scanning.
- Return non-zero status for execution failures.
- Distinguish complete and partial scans.
- Lock behavior with released-binary style CLI tests.
- Refresh public security and community contracts.

## Completion criteria

- [ ] Every child issue in this milestone is closed or explicitly deferred with rationale.
- [ ] go test ./..., go build ./..., and go vet ./... pass.
- [ ] A release decision is recorded without implying v1.0 readiness.
