---
id: AIB-63
title: Auto-enter guided cleanup from default clean
status: Done
labels:
  - enhancement
  - ux
  - cli
  - safety
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
completed_date: '2026-07-10'
---
## Description
Part of #62

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

In v0.6.0, the smarter Codex worktree analysis exists behind `aibris clean --guide`. In real CLI use, users run `aibris clean` or `aibris clean --dry-run`; the default path should surface the best decision flow automatically when Codex worktrees are a meaningful cleanup opportunity.

## Scope

- Route plain `aibris clean` into guided cleanup when no explicit cleanup filters are supplied and the scan finds meaningful Codex worktree candidates.
- Route plain `aibris clean --dry-run` the same way, preserving dry-run semantics.
- Keep the existing guide analysis and recommendation model as the initial renderer for v0.6.1.
- Preserve final deletion confirmation for non-dry-run guided cleanup.
- Do not change explicit classic cleanup behavior when the user names concrete filters.

## Acceptance Criteria

- [x] `aibris clean` with valuable Codex worktree candidates enters guided cleanup by default.
- [x] `aibris clean --dry-run` shows the guided plan and deletes nothing.
- [x] The guided default explains why each recommended item is low-risk before asking for confirmation.
- [x] The old deletion guardrails still apply: no dry-run means confirmation unless `--force` is explicitly used.
- [x] Tests cover default routing for clean and dry-run.

## Completion Evidence

- PR #71 merged as `e67078b`.
- Issue #63 closed as completed.
