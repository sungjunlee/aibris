---
id: AIB-3
title: Exclude active worktrees from cleanup by default
status: Done
labels: [safety, cli]
priority: critical
milestone: Scan + Clean Safety
dependencies: []
created_date: '2026-06-01'
github_issue: 23
github_url: https://github.com/sungjunlee/aibris/issues/23
---

## Description

Use existing `DebrisInfo.Status` in cleanup filtering so active worktrees are
not deleted just because they are old. This closes the biggest safety gap in
the current cleanup behavior.

Default policy:

```text
orphaned worktree -> eligible when age/category/tool filters match
active worktree   -> excluded by default
plain-dir         -> ignored by scanner
```

Add `--include-active-worktrees` for users who explicitly want the old
age-based behavior.

## Acceptance Criteria

<!-- AC:BEGIN -->
- [x] `cleaner.Filter` excludes `CategoryWorktree` items with `Status=active` by default.
- [x] `cleaner.Filter` includes `Status=orphaned` worktrees when age/category/tool filters match.
- [x] Non-worktree filtering behavior is unchanged.
- [x] `clean` exposes `--include-active-worktrees`.
- [x] `aibris clean --category worktree --dry-run` omits active worktrees.
- [x] `aibris clean --include-active-worktrees --category worktree --dry-run` includes active worktrees when age filters match.
- [x] Tests cover active, orphaned, empty status, non-worktree, dry-run, and CLI flag behavior.
- [x] `go test ./...`, `go build ./...`, and `go vet ./...` pass.
<!-- AC:END -->
