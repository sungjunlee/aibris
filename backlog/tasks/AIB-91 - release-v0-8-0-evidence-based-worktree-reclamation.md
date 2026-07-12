---
id: AIB-91
title: Release v0.8.0 evidence-based worktree reclamation
status: To Do
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
- [ ] go test ./..., go build ./..., and go vet ./... pass.
- [ ] goreleaser release --snapshot --clean passes.
- [ ] Annotated v0.8.0 tag and GitHub Release assets publish successfully.
- [ ] Installer smoke reports aibris version 0.8.0.
