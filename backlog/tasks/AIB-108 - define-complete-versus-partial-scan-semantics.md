---
id: AIB-108
title: Define complete versus partial scan semantics
status: In Progress
labels:
  - documentation
  - enhancement
  - ux
  - cli
  - scanner
  - type:bug
  - area:automation
priority: high
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
---
## Description
## Problem

A provider failure can be written to stderr while scan still returns a valid-looking partial result. Machine consumers cannot tell whether the inventory is complete.

## Acceptance criteria

- [ ] Human output clearly labels partial scans and failed providers.
- [ ] JSON output exposes machine-readable completeness and provider errors without leaking unrelated data.
- [ ] Exit-status behavior for usable partial results is explicitly documented and tested.
- [ ] clean never treats an unsafe or incomplete prerequisite as stronger evidence than it has.
- [ ] Cancellation remains a hard failure.
- [ ] Existing successful scan output stays stable unless intentionally versioned.
- [ ] go test ./..., go build ./..., and go vet ./... pass.
