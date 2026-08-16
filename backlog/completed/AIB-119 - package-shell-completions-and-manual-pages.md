---
id: AIB-119
title: Package shell completions and manual pages
status: To Do
labels:
  - documentation
  - enhancement
  - devops
  - area:oss
  - cli
  - type:chore
  - area:automation
  - area:release
priority: medium
milestone: '0.x OSS Distribution & Release Trust'
created_date: '2026-07-22'
---
## Description
## Goal

Turn Cobra-generated help into discoverable shell and manual integration.

## Acceptance criteria

- [ ] Release packaging includes bash, zsh, fish, and PowerShell completions where supported.
- [ ] Homebrew or installer integration places completions in standard locations without modifying shell profiles silently.
- [ ] A concise man page covers scan, clean, safety gates, and exit status.
- [ ] Generated artifacts are reproducible from source.
- [ ] Installation and uninstall behavior is documented.
