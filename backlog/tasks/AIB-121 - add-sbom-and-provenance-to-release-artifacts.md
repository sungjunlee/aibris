---
id: AIB-121
title: Add SBOM and provenance to release artifacts
status: Done
labels:
  - documentation
  - security
  - devops
  - type:chore
  - area:release
priority: high
milestone: '0.x OSS Distribution & Release Trust'
created_date: '2026-07-22'
---
## Description
## Goal

Raise supply-chain trust for a tool that can permanently delete local files.

## Acceptance criteria

- [x] Every release publishes an SBOM for distributed binaries.
- [x] GitHub artifact attestations or equivalent provenance are generated from the release workflow.
- [x] Verification steps are documented with copy-paste commands.
- [x] Checksums remain published and verified by install.sh.
- [x] Release permissions stay least-privilege.
- [x] A failed provenance step prevents publishing an incomplete trusted release.
