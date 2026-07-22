---
id: AIB-118
title: Publish and verify a Homebrew installation path
status: To Do
labels:
  - documentation
  - devops
  - area:oss
  - type:chore
  - area:release
priority: high
milestone: '0.x OSS Distribution & Release Trust'
created_date: '2026-07-22'
---
## Description
## Goal

Make the recommended macOS and Linux installation a package-manager command while retaining checksummed release binaries.

## Acceptance criteria

- [ ] A maintained tap or accepted formula installs the latest stable 0.x release.
- [ ] brew install and brew upgrade are smoke-tested in CI or a release workflow.
- [ ] Version output matches the release tag.
- [ ] README documents ownership and trust boundaries for third-party formulas if applicable.
- [ ] The existing install.sh path remains supported.
