---
id: AIB-4
title: Add derived status, risk, and reason fields to scan JSON
status: In Review
labels: [scanner, cli]
priority: medium
milestone: Scan + Clean Safety
dependencies: [AIB-3]
created_date: '2026-06-01'
github_issue: 24
github_url: https://github.com/sungjunlee/aibris/issues/24
---

## Description

Improve `scan --json` so AI assistants and users can understand why an item is
or is not a good cleanup candidate.

Add presentation-only JSON fields derived from existing model state:

- `status`
- `risk`
- `reason`

Do not add stored risk state to `DebrisInfo` in this pass. Keep the historical
top-level `worktrees` array for compatibility.

## Acceptance Criteria

<!-- AC:BEGIN -->
- [ ] JSON output includes `status` for worktree items.
- [ ] JSON output includes derived `risk` for every item.
- [ ] JSON output includes a short derived `reason` for every item.
- [ ] Active worktree reason explains that it is protected by default.
- [ ] Orphaned worktree reason explains that parent repo metadata is missing.
- [ ] Existing top-level `worktrees` array and current fields remain backward-compatible.
- [ ] Tests cover JSON output for active worktree, orphaned worktree, node_modules, build-cache, other-cache, and ai-logs.
- [ ] `go test ./...`, `go build ./...`, and `go vet ./...` pass.
<!-- AC:END -->
