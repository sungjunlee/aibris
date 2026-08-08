---
id: AIB-117
title: Prepare a 0.9.x unified-cleanup release
status: Done
labels:
  - documentation
  - devops
  - cli
  - safety
  - type:chore
  - area:release
priority: high
milestone: 0.9.x Unified Cleanup Experience
created_date: '2026-07-22'
completed_date: '2026-08-08'
---
## Description
## Goal

Publish the unified cleanup experience as a 0.9.x release when it feels complete, without setting a date or treating it as a v1 release candidate.

## Acceptance criteria

- [x] All release-blocking unified-cleanup issues are closed.
- [x] Upgrade and compatibility notes explain any default-flow change.
- [x] CI, race tests, build, vet, installer smoke test, and dogfood pass.
- [x] Release notes are user-facing and include safety behavior.
- [x] The maintainer explicitly approves the experience before tagging.

## Completion evidence

- Maintainer approval resumed the release gate on 2026-08-08 and selected
  v0.9.0 rather than another 0.8.x patch because the default guided flow now
  uses one mixed-category review and execution contract.
- PR #195 merged as `03a108d`. Four fresh-context review rounds found and
  fixed three cached-evidence expiry blockers before the final round passed
  with no blocker, P1, or P2 findings.
- CI run `31250722048` passed Ubuntu/macOS race tests, Windows safety, and the
  release cross-build. Local full race, build, vet, curated Windows-note
  validation, and six-target GoReleaser snapshot gates also passed.
- Annotated tag `v0.9.0` points to `03a108d`; release workflow `31250841468`
  published curated notes, six platform archives, and `checksums.txt`.
- `install.sh 0.9.0` downloaded the public Darwin arm64 archive into an
  isolated prefix, verified the published checksum, and reported
  `aibris version 0.9.0`.
- The installed public binary completed a real-home `scan --json` with 183
  debris items, no partial state, and zero provider errors. Plain
  `clean --dry-run` reported 18 found / 13 eligible / 9 selected / 4
  reviewable / 5 protected and ended with
  `[DRY-RUN] No files were removed.` No deletion-mode command was run.
