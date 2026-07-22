---
id: AIB-107
title: Propagate cleanup failures through process exit status
status: In Progress
labels:
  - cli
  - safety
  - type:bug
  - area:automation
priority: critical
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
---
## Description
## Problem

Classic and interactive cleanup paths can print execution errors but still return process exit status 0. Scripts and agents may treat partial or total deletion failure as success.

## Acceptance criteria

- [ ] Classic cleanup returns non-zero when any selected target fails.
- [ ] Interactive cleanup returns non-zero when an approved target fails while still reporting successful targets.
- [ ] Guided, classic, and interactive receipts use the same success and partial-failure semantics.
- [ ] User cancellation remains distinct from execution failure.
- [ ] Receipt output is printed before the final error is returned.
- [ ] Tests cover total failure, partial failure, cancellation, and success.
- [ ] go test ./..., go build ./..., and go vet ./... pass.
