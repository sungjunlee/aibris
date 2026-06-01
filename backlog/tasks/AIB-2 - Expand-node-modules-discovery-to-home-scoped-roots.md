---
id: AIB-2
title: Expand node_modules discovery to home-scoped roots
status: In Review
labels: [scanner]
priority: high
milestone: Scan + Clean Safety
dependencies: [AIB-1]
created_date: '2026-06-01'
github_issue: 22
github_url: https://github.com/sungjunlee/aibris/issues/22
---

## Description

Change `NodeModulesAdapter` from scanning only `~/projects` to walking the
configured scan roots. Default behavior should discover `node_modules` under
common home locations like `~/workspace`, `~/Developer`, `~/src`, `Desktop`,
and `Downloads`.

To keep `$HOME` scanning practical, the walk must prune high-noise directories:
`.Trash`, `Library`, `Applications`, `Pictures`, `Movies`, `Music`,
`node_modules`, `.git`, and `vendor`.

## Acceptance Criteria

<!-- AC:BEGIN -->
- [ ] `aibris scan` finds `node_modules` outside `~/projects` under default `$HOME` roots.
- [ ] `aibris scan --root <home-subdir>` only reports `node_modules` under that root.
- [ ] The adapter skips traversal into nested `node_modules`.
- [ ] The adapter prunes `.Trash`, `Library`, `Applications`, `Pictures`, `Movies`, `Music`, `.git`, and `vendor`.
- [ ] The adapter does not prune `Desktop` or `Downloads` by default.
- [ ] Context cancellation is still respected during the walk.
- [ ] Tests cover default `$HOME` scan, custom roots, pruned roots, `Desktop`/`Downloads`, nested `node_modules`, and cancellation.
- [ ] `go test ./...`, `go build ./...`, and `go vet ./...` pass.
<!-- AC:END -->
