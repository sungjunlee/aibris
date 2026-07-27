---
id: AIB-140
title: Replace bounded worktree discovery with a container registry
status: To Do
labels:
  - enhancement
  - cli
  - scanner
  - type:feature
priority: high
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
---
## Description
## Goal

Find agent worktree containers regardless of nesting depth and member layout.
`~/.config/superpowers/worktrees` held 516 MB in two valid linked worktrees and
scan discovered none of it: the container sits at depth 3 under a hidden owner
rather than being its immediate child, and members use a
`worktrees/<project>/<worktree>` layout rather than `worktrees/<worktree>`.
`~/.relay/worktrees` also reported only 3 of 7 entries.

## Acceptance criteria

- [ ] Known containers are discovered regardless of nesting depth under a hidden
      owner. Direct measurement on 2026-07-26 confirmed this is the **only** real
      gap: `~/.config/superpowers/worktrees` sits at depth 3 under `~/.config/`
      rather than being an immediate child of a hidden owner, so it is never
      reached. Both L1 and L2 member layouts already work, and `~/.relay/worktrees`
      discovery is already complete — the earlier "3 of 7" figure compared a fresh
      scan against a stale directory listing.
- [ ] `~/.config/superpowers/worktrees` members are discovered completely; record
      before/after counts in one run, not across runs, because relay's own
      worktrees live under `~/.relay/worktrees` and change mid-session.
- [ ] Both `worktrees/<unit>/.git` and `worktrees/<unit>/<repo>/.git` layouts keep
      working — a regression guard, not new behavior.
- [ ] Discovery stays bounded with no unbounded full-home recursion; the bound is
      documented.
- [ ] Container entries without valid Git metadata are reported honestly rather
      than silently dropped.
- [ ] Full-home scan time is reported against the 19.2s baseline.
