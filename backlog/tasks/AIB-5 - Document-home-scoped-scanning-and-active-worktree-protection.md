---
id: AIB-5
title: Document home-scoped scanning and active worktree protection
status: Done
labels: [docs]
priority: medium
milestone: Scan + Clean Safety
dependencies: [AIB-1, AIB-2, AIB-3, AIB-4]
created_date: '2026-06-01'
github_issue: 25
github_url: https://github.com/sungjunlee/aibris/issues/25
---

## Description

Update public docs and the AI-guided cleanup skill so users understand the new
scan scope and worktree safety behavior.

The docs should be explicit: default scan starts at `$HOME`, prunes known noisy
directories, rejects roots outside `$HOME`, and excludes active worktrees from
cleanup unless `--include-active-worktrees` is set.

## Acceptance Criteria

<!-- AC:BEGIN -->
- [x] `README.md` documents `$HOME` default scanning, `--root`, pruning rules, and active worktree protection.
- [x] `docs/SPEC.md` documents scan roots, root validation, and worktree cleanup policy.
- [x] `docs/JSON_SCHEMA.md` documents `status`, `risk`, and `reason`.
- [x] `docs/CATEGORY.md` documents active versus orphaned worktree behavior.
- [x] `skills/aibris/SKILL.md` updates the AI-guided workflow to prioritize orphaned worktrees and warn before active worktree cleanup.
- [x] Examples include `aibris scan --root ~/workspace --json` and `aibris clean --category worktree --dry-run`.
- [x] `go test ./...`, `go build ./...`, and `go vet ./...` pass after docs changes.
<!-- AC:END -->
