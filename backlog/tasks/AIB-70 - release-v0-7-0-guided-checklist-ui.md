---
id: AIB-70
title: Release v0.7.0 guided checklist UI
status: In Progress
labels:
  - documentation
  - devops
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
---
## Description
Part of #62

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

The interactive checklist changes the main cleanup experience enough to warrant a minor release with clear notes, examples, and verification.

## Scope

- Bump version to v0.7.0 after checklist implementation lands.
- Update changelog/release notes with checklist behavior, fallback mode, and safety guarantees.
- Verify build/vet/tests and dogfood the terminal UI.
- Create and push the git tag and GitHub release.

## Acceptance Criteria

- [x] Version metadata reflects v0.7.0.
- [x] Changelog/release notes describe checklist selection behavior and defaults.
- [x] `go build ./...` passes.
- [x] `go vet ./...` passes.
- [ ] Git tag and GitHub release exist for v0.7.0.
