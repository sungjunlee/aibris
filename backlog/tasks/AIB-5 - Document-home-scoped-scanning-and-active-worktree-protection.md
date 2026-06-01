---
id: AIB-5
title: Document home-scoped scanning and active worktree protection
status: In Review
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
- [ ] `README.md` documents `$HOME` default scanning, `--root`, pruning rules, and active worktree protection.
- [ ] `docs/SPEC.md` documents scan roots, root validation, and worktree cleanup policy.
- [ ] `docs/JSON_SCHEMA.md` documents `status`, `risk`, and `reason`.
- [ ] `docs/CATEGORY.md` documents active versus orphaned worktree behavior.
- [ ] `skills/aibris/SKILL.md` updates the AI-guided workflow to prioritize orphaned worktrees and warn before active worktree cleanup.
- [ ] Examples include `aibris scan --root ~/workspace --json` and `aibris clean --category worktree --dry-run`.
- [ ] `go test ./...`, `go build ./...`, and `go vet ./...` pass after docs changes.
<!-- AC:END -->
