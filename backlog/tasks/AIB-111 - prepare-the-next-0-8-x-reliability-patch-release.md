---
id: AIB-111
title: Prepare the next 0.8.x reliability patch release
status: To Do
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
---
## Description
## Goal

Ship the reliability work as a 0.8.x patch only after the milestone contracts are verified. This issue does not set a fixed date or commit the project to v1.0.

## Acceptance criteria

- [ ] All release-blocking issues in 0.8.x Reliability & Trust are closed.
- [ ] CHANGELOG.md contains user-facing Added, Changed, Fixed, and Safety notes as applicable.
- [ ] go test -race -count=1 -cover ./..., go build ./..., and go vet ./... pass.
- [ ] install.sh is smoke-tested against the published release assets and checksums.
- [ ] GitHub Release notes use the curated changelog rather than an unfiltered commit dump.
- [ ] Post-release scan and dry-run dogfood evidence is recorded without deleting real user data.

## Release decision

Deferred on 2026-07-23 pending explicit maintainer approval. Do not create a
tag, date, or release commitment merely because #105-#110 are complete.
Published-asset installation verification and post-release read-only dogfood
remain intentionally open until an actual 0.8.x patch is approved.
