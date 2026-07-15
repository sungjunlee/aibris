---
id: AIB-91
title: Release v0.8.0 evidence-based worktree reclamation
status: Done
labels:
  - documentation
  - devops
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
completed_date: '2026-07-13'
---
## Description
## Parent

- Epic: #81
- Milestone: Evidence-Based Worktree Reclamation
- PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md

## Dependencies

#90

## Scope

- Prepare version and changelog updates.
- Run full validation and GoReleaser snapshot.
- Publish tag and GitHub Release after CI passes.
- Smoke-test the installer in a temporary prefix.

## Acceptance Criteria

- [x] All milestone implementation issues are closed or explicitly deferred.
- [x] go test ./..., go build ./..., and go vet ./... pass.
- [x] goreleaser release --snapshot --clean passes.
- [x] Annotated v0.8.0 tag and GitHub Release assets publish successfully.
- [x] Installer smoke reports aibris version 0.8.0.

## Preparation Evidence

- 2026-07-13: Prepared from `origin/main` at `5066289`. Merged PRs #92-#100
  map to implementation issues #82-#90; the release orchestrator verified all
  nine issues closed through the GitHub API.
- 2026-07-13: Added dated v0.8.0 changelog notes covering cleanup units,
  evidence-based policy, Git-aware removal, and hard-safety boundaries. Version
  remains tag-derived through the existing GoReleaser ldflags.
- 2026-07-13: `go test ./...`, `go build ./...`, and `go vet ./...` passed with
  `GOCACHE` redirected to the executor's writable temporary directory.
- 2026-07-13: The executor's network-isolated snapshot attempt identified
  uncached module dependencies. The release orchestrator then ran
  `goreleaser release --snapshot --clean` with normal network access; it
  succeeded for all six Darwin/Linux/Windows amd64/arm64 archives, emitted
  `checksums.txt`, and recorded tag-derived `v0.7.0-next` snapshot metadata.
- 2026-07-13: Annotated tag `v0.8.0` points to release commit `3d1dad3`.
  Release workflow `29201631207` published six platform archives and
  `checksums.txt` successfully.
- 2026-07-13: The tagged installer installed into a disposable prefix and both
  its smoke output and the installed binary reported `aibris version 0.8.0`.
