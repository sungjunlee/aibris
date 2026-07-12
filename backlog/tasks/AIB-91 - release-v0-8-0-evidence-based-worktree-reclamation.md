---
id: AIB-91
title: Release v0.8.0 evidence-based worktree reclamation
status: In Progress
labels:
  - documentation
  - devops
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
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

- [ ] All milestone implementation issues are closed or explicitly deferred.
- [x] go test ./..., go build ./..., and go vet ./... pass.
- [ ] goreleaser release --snapshot --clean passes.
- [ ] Annotated v0.8.0 tag and GitHub Release assets publish successfully.
- [ ] Installer smoke reports aibris version 0.8.0.

## Preparation Evidence

- 2026-07-13: Prepared from `origin/main` at `5066289`. Merged PRs #92-#100
  map to implementation issues #82-#90 and are present in the local history.
  GitHub issue closed-state verification remains unchecked because
  `api.github.com` was unreachable from the executor.
- 2026-07-13: Added dated v0.8.0 changelog notes covering cleanup units,
  evidence-based policy, Git-aware removal, and hard-safety boundaries. Version
  remains tag-derived through the existing GoReleaser ldflags.
- 2026-07-13: `go test ./...`, `go build ./...`, and `go vet ./...` passed with
  `GOCACHE` redirected to the executor's writable temporary directory.
- 2026-07-13: `goreleaser release --snapshot --clean` reached the configured
  `go mod tidy` hook, then failed because the network-isolated executor could
  not download `github.com/cpuguy83/go-md2man/v2@v2.0.6`. A diagnostic run with
  the hook skipped confirmed tag-derived `v0.7.0-next` metadata and ldflags in
  four Unix binaries, then stopped on the uncached Windows-only `mousetrap`
  dependency; no archives, checksum file, or complete asset set was produced.
- Tag creation, GitHub Release publication, and installer smoke testing are
  intentionally left to the release orchestrator.
