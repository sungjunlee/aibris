---
id: AIB-111
title: Prepare the next 0.8.x reliability patch release
status: Done
labels:
  - documentation
  - devops
  - area:oss
  - safety
  - type:chore
  - area:release
priority: medium
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
completed_date: '2026-07-31'
---
## Description
## Goal

Ship the reliability work as a 0.8.x patch only after the milestone contracts are verified. This issue does not set a fixed date or commit the project to v1.0.

## Acceptance criteria

- [x] All release-blocking issues in 0.8.x Reliability & Trust are closed.
- [x] CHANGELOG.md contains user-facing Added, Changed, Fixed, and Safety notes as applicable.
- [x] go test -race -count=1 -cover ./..., go build ./..., and go vet ./... pass.
- [x] install.sh is smoke-tested against the published release assets and checksums.
- [x] GitHub Release notes use the curated changelog rather than an unfiltered commit dump.
- [x] Post-release scan and dry-run dogfood evidence is recorded without deleting real user data.

## Historical release decision (2026-07-23)

The release was deferred on 2026-07-23 pending explicit maintainer approval.
No tag, date, or release commitment was created merely because #105-#110 were
complete. Published-asset installation verification and post-release read-only
dogfood remained intentionally open until the maintainer resumed this task on
2026-07-31.

## Completion evidence

- Maintainer approval resumed the release on 2026-07-31.
- PR #169 merged as `2fa7a04`; annotated tag `v0.8.1` points to that commit.
- Release workflow `30608576573` published the curated v0.8.1 notes, six
  Darwin/Linux/Windows archives, and `checksums.txt`.
- Full race tests, build, vet, GoReleaser validation, and a six-target snapshot
  build passed. Every snapshot checksum matched.
- The tagged installer downloaded the public Darwin arm64 archive into an
  isolated temporary prefix, verified its checksum, and reported
  `aibris version 0.8.1`.
- The installed public binary completed a real-home `scan --json` and
  `clean --dry-run`. Guided review selected 0 items / 0 B and protected 12
  items / 1.1 GB. The unified classic audit reported 213 eligible physical
  targets / 1.6 GB and 160 protected or skipped targets / 30.6 GB. The run
  ended with
  `[DRY-RUN] No files were removed.` No deletion-mode clean was run.
