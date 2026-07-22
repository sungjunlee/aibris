---
id: AIB-120
title: Establish and test the Windows support contract
status: To Do
labels:
  - documentation
  - devops
  - type:chore
  - area:release
priority: high
milestone: '0.x OSS Distribution & Release Trust'
created_date: '2026-07-22'
---
## Description
## Problem

GoReleaser publishes Windows archives, but pull-request CI currently tests only macOS and Linux and the Bash installer does not serve native Windows users.

## Acceptance criteria

- [ ] Decide and document supported Windows versions and shells, or explicitly mark Windows binaries experimental.
- [ ] windows-latest CI builds and runs platform-safe command tests.
- [ ] Home and path safety behavior is verified on Windows path semantics.
- [ ] Installation instructions cover a native Windows path or explain manual installation.
- [ ] Unsupported adapters or cache locations are reported honestly.
- [ ] Release publishing matches the documented support level.
