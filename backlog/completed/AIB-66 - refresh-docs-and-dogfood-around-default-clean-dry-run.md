---
id: AIB-66
title: Refresh docs and dogfood around default clean --dry-run
status: Done
labels:
  - documentation
  - ux
  - docs
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
completed_date: '2026-07-10'
---
## Description
Part of #62

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

The user-facing documentation and dogfood evidence need to match the new product posture: plain `clean` is the guided decision path, and explicit flags are secondary controls.

## Scope

- Update README/help examples from `clean --guide`-centric usage to plain `clean` / `clean --dry-run` usage.
- Document `--no-guide` and the classic executor path.
- Add dogfood notes or evidence showing default guided behavior against a realistic Codex worktree-heavy environment.
- Keep the PRD linked from implementation/release notes.

## Acceptance Criteria

- [x] README examples show `aibris clean --dry-run` as the natural first command.
- [x] Documentation explains when `--no-guide` is appropriate.
- [x] Help text and docs agree on routing semantics.
- [x] Dogfood evidence records guided recommendations and selected deletion impact.
- [x] Documentation avoids presenting `--guide` as a feature users must know in advance.

## Completion Evidence

- PR #73 merged as `edfa80f`.
- Issue #66 closed as completed.
