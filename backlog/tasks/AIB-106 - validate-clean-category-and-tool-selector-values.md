---
id: AIB-106
title: Validate clean category and tool selector values
status: In Progress
labels:
  - cli
  - type:bug
priority: high
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
---
## Description
## Problem

Unknown values such as --category mystery and --tool mystery currently produce an empty successful result with exit status 0. Typos look like valid no-op cleanups.

## Acceptance criteria

- [ ] Unknown category values fail before scanning.
- [ ] Unknown tool values fail before scanning.
- [ ] Errors include the invalid value and the valid choices.
- [ ] Comma-separated values are trimmed, deduplicated, and validated independently.
- [ ] Existing valid category and tool combinations remain compatible.
- [ ] Command tests assert stderr and non-zero exit behavior.
- [ ] go test ./..., go build ./..., and go vet ./... pass.
