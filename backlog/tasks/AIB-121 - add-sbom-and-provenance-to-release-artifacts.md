---
id: AIB-121
title: Add SBOM and provenance to release artifacts
status: To Do
labels:
  - documentation
  - security
  - devops
  - type:chore
  - area:release
priority: medium
milestone: '0.x OSS Distribution & Release Trust'
created_date: '2026-07-22'
---
## Description
## Goal

Raise supply-chain trust for a tool that can permanently delete local files.

## Acceptance criteria

- [ ] Every release publishes an SBOM for distributed binaries.
- [ ] GitHub artifact attestations or equivalent provenance are generated from the release workflow.
- [ ] Verification steps are documented with copy-paste commands.
- [ ] Checksums remain published and verified by install.sh.
- [ ] Release permissions stay least-privilege.
- [ ] A failed provenance step prevents publishing an incomplete trusted release.
