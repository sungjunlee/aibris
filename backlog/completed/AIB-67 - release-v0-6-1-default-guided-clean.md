---
id: AIB-67
title: Release v0.6.1 default guided clean
status: Done
labels:
  - documentation
  - devops
  - cli
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
completed_date: '2026-07-10'
---
## Description
Part of #62

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

Default guided cleanup is a meaningful CLI behavior change and should ship as a patch release with explicit release notes and tag verification.

## Scope

- Bump version to v0.6.1 after implementation and docs land.
- Update changelog/release notes with default guided cleanup behavior and opt-out semantics.
- Run required verification before tagging.
- Create and push the git tag and GitHub release.

## Acceptance Criteria

- [x] Version metadata reflects v0.6.1.
- [x] Changelog/release notes call out default guided clean and `--no-guide`.
- [x] `go build ./...` passes.
- [x] `go vet ./...` passes.
- [x] Git tag and GitHub release exist for v0.6.1.

## Completion Evidence

- PR #74 merged as `788c69b`.
- Release workflow `29057968415` succeeded.
- GitHub Release `v0.6.1` exists with published assets.
- Issue #67 closed as completed.
