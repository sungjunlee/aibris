---
id: AIB-1
title: Add explicit scan roots and root validation
status: Done
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
- [x] `scan` accepts repeated `--root` flags.
- [x] `clean` accepts repeated `--root` flags and uses the same scan scope as `scan`.
- [x] Default roots are equivalent to resolved `$HOME`.
- [x] Roots expand `~`, resolve symlinks when possible, and reject paths outside resolved `$HOME`.
- [x] Roots are sorted, deduplicated, and nested roots are dropped when an ancestor root is already present.
- [x] `DebrisProvider` receives explicit `types.ScanOptions` instead of reading implicit global scan scope.
- [x] Existing `scanner.Scan(ctx)` compatibility wrapper still works.
- [x] Tests cover valid roots, invalid roots outside `$HOME`, symlink escape, duplicate roots, and nested roots.
- [x] `go test ./...`, `go build ./...`, and `go vet ./...` pass.
<!-- AC:END -->
