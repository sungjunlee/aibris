---
id: AIB-148
title: Prepare v0.8.5 release (retention inventory, atomic cache, docs reframe)
status: Done
labels:
  - devops
  - documentation
  - area:oss
  - area:release
  - type:chore
priority: medium
milestone: '0.8.x Reliability & Trust'
created_date: '2026-08-06'
completed_date: '2026-08-06'
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
- [x] `go test -race -count=1 -cover ./...`, `go build ./...`, and
      `go vet ./...` pass.
- [x] Real-home dogfood scan with the retention inventory is recorded (see
      Completion evidence).
- [x] Annotated tag `v0.8.5` and release workflow `31107451585` published the
      curated notes, six archives, and `checksums.txt`.
- [x] `install.sh v0.8.5` smoke-tested: downloaded the public
      `aibris_darwin_arm64.tar.gz`, verified the published checksum, installed
      into an isolated prefix, and reported `aibris version 0.8.5`.
- [x] Post-release dogfood recorded in `docs/DOGFOOD.md` without deleting any
      real user data: scan (185 items, retention 12 buckets / 7,479 units /
      14.24 GB / 272 orphaned) and `clean --dry-run --no-guide` ending with
      `[DRY-RUN] No files were removed.`

## Completion evidence

- Real-home dogfood scan, 2026-08-06 (current `main`, `schema_version` 1):
  12 UTC-month retention buckets (`2025-09` .. `2026-08`), 7,479 units,
  14.24 GB, 272 orphaned units / 99.8 MB; `retention.partial` false, zero
  provider errors; debris scan complete (184 items) with no top-level partial
  state.
