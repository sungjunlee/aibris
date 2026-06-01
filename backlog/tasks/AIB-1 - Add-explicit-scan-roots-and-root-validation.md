---
id: AIB-1
title: Add explicit scan roots and root validation
status: In Review
labels: [cli, scanner, safety]
priority: high
milestone: Scan + Clean Safety
dependencies: []
created_date: '2026-06-01'
github_issue: 21
github_url: https://github.com/sungjunlee/aibris/issues/21
---

## Description

Introduce explicit scan roots so `aibris scan` and `aibris clean` can operate
on `$HOME` by default and on user-provided roots when requested.

This is the foundation for fixing the current hardcoded `~/projects`
assumption. The CLI should accept repeated `--root` flags, normalize those
roots, and pass them through the scanner contract to providers.

## Acceptance Criteria

<!-- AC:BEGIN -->
- [ ] `scan` accepts repeated `--root` flags.
- [ ] `clean` accepts repeated `--root` flags and uses the same scan scope as `scan`.
- [ ] Default roots are equivalent to resolved `$HOME`.
- [ ] Roots expand `~`, resolve symlinks when possible, and reject paths outside resolved `$HOME`.
- [ ] Roots are sorted, deduplicated, and nested roots are dropped when an ancestor root is already present.
- [ ] `DebrisProvider` receives explicit `types.ScanOptions` instead of reading implicit global scan scope.
- [ ] Existing `scanner.Scan(ctx)` compatibility wrapper still works.
- [ ] Tests cover valid roots, invalid roots outside `$HOME`, symlink escape, duplicate roots, and nested roots.
- [ ] `go test ./...`, `go build ./...`, and `go vet ./...` pass.
<!-- AC:END -->
