---
id: AIB-67
title: Release v0.6.1 default guided clean
status: To Do
labels:
  - documentation
  - devops
  - cli
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
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

- [ ] Version metadata reflects v0.6.1.
- [ ] Changelog/release notes call out default guided clean and `--no-guide`.
- [ ] `go build ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] Git tag and GitHub release exist for v0.6.1.

