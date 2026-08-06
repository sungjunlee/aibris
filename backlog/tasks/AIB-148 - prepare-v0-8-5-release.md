---
id: AIB-148
title: Prepare v0.8.5 release (retention inventory, atomic cache, docs reframe)
status: In Progress
labels:
  - devops
  - documentation
  - area:oss
  - area:release
  - type:chore
priority: medium
milestone: '0.8.x Reliability & Trust'
created_date: '2026-08-06'
---
## Description

## Goal

Ship v0.8.5: the read-only codex-sessions retention inventory (#139/#187), the
atomic last-scan cache write (#185), provider-registry overlap fingerprint
roots (#186), and the product-boundary docs reframe (#143/#188). Follow the
AIB-111 release discipline: curated changelog, full race/build/vet gate,
installer smoke test, and post-release read-only dogfood.

## Acceptance criteria

- [x] CHANGELOG.md contains user-facing Added, Changed, and Safety notes.
- [x] `.github/release-notes/v0.8.5.md` is curated (not a commit dump) and
      passes the Windows status validation.
- [x] `go test -race -count=1 ./...`, `go build ./...`, and `go vet ./...`
      pass.
- [ ] Real-home dogfood scan with the retention inventory is recorded
      (2026-08-06: 12 UTC-month buckets, 7,479 units, 14.24 GB, 272 orphaned
      units; debris scan complete, no partial state).
- [ ] Annotated tag `v0.8.5` and the release workflow publish the curated
      notes, archives, and checksums.
- [ ] `install.sh` is smoke-tested against the published assets and checksums.
- [ ] Post-release scan/dry-run dogfood evidence is recorded without deleting
      real user data.
