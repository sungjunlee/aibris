---
id: AIB-6
title: Prefer official cache cleanup commands where safe
status: In Review
labels: [tech-debt, safety]
priority: medium
milestone: Future
dependencies: [AIB-1, AIB-5]
created_date: '2026-06-01'
github_issue: 26
github_url: https://github.com/sungjunlee/aibris/issues/26
---

## Description

Follow-up to the scan and cleanup safety work. For caches that provide official
maintenance commands, prefer the owning tool over direct path deletion.

Candidate commands:

- `uv cache prune`
- `go clean -cache`
- npm cache maintenance commands

This is intentionally not part of the first scan-safety PR because command
cleanup has different semantics from path deletion and needs separate review.

## Acceptance Criteria

<!-- AC:BEGIN -->
- [ ] Design a command cleanup strategy that uses argv-only execution, never shell strings.
- [ ] Define exact dry-run behavior for command-backed cleanup.
- [ ] Define fallback rules when a command is missing or fails.
- [ ] Use context cancellation for command execution.
- [ ] Preserve path safety checks for any fallback path deletion.
- [ ] Tests cover successful command execution, missing command, failed command, timeout/cancellation, and fallback behavior.
- [ ] Docs explain which caches use official commands and which still use path deletion.
- [ ] `go test ./...`, `go build ./...`, and `go vet ./...` pass.
<!-- AC:END -->
