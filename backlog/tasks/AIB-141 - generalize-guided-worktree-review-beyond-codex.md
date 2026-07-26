---
id: AIB-141
title: Generalize guided worktree review beyond codex
status: To Do
labels:
  - enhancement
  - cli
  - safety
  - type:feature
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
---
## Description
## Goal

Let guided review admit worktree units from any agent tool. The largest single
worktree on the audited home was a `claude` worktree, 1.5 GB, idle 93 days. The
classic route protects it because it reports `active`, and guided review skips it
because guided review is codex-only, so no default path can reach it.

## Acceptance criteria

- [ ] Guided review admits worktree units from any tool.
- [ ] Git evidence alone is sufficient to build a review row; tool-specific
      activity evidence refines the decision but is not required to produce one.
- [ ] A tool with no activity source yields an explicit reason rather than a
      silent lock or a silent recommendation.
- [ ] Every existing hard lock is preserved: cwd containment, dirty or untracked
      members, unreadable evidence, detached HEAD unreachable from named refs,
      and the 6-hour recent-activity window.
- [ ] The non-forced `git worktree remove` contract, preflight, and verification
      behavior are unchanged.
- [ ] `--guide` no longer implies `--tool codex`; documented in README and SPEC.
- [ ] Regression test: a non-codex worktree idle beyond the minimum idle age
      reaches a review row.
