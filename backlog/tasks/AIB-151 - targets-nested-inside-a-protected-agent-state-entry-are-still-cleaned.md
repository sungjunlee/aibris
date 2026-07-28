---
id: AIB-151
title: Targets nested inside a protected agent-state entry are still cleaned
status: To Do
labels:
  - bug
  - cli
  - scanner
  - safety
  - type:bug
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-28'
---
## Description
## Problem

A `node_modules` target nested inside a **protected** `agent-state` entry is
still planned for deletion by default `clean`.

Measured on a real home:

```
node_modules 126,976 B inside agent-state(live)         Users-sjlee-workspace-active-harness-stack-dev-relay
node_modules 126,976 B inside agent-state(undetermined) Users-sjlee-workspace-active-finance-stack-knestfin
node_modules 126,976 B inside agent-state(undetermined) empty-window
```

`~/.cursor/projects/<key>/canvases/node_modules` is removed while the enclosing
store is classified `live` or `undetermined` and is deliberately protected from
deletion. The protection is on the entry, not on its contents.

## Pre-existing, not introduced by the migration

Confirmed on `main`: the same three targets are planned there too, when cursor
entries were still `ai-logs`. #138 L2 does not change this. Filing it separately
so the migration is not blamed for it.

## Why it will get worse

The agent-state surface is expanding (#139, #142). More stores means more
opportunities for a generic provider to reach inside a protected entry. The
inverse case — a generic target *containing* an orphaned agent-state entry —
also needs a decision, since #138's own acceptance criteria ask that "overlap
accounting stays correct when an orphan sits inside another target".

## Questions to settle

- Should a protected `agent-state` entry shield its whole subtree from other
  providers, or is deleting reinstallable `node_modules` inside a live store
  acceptable and merely surprising?
- When an orphaned entry contains another target, which one owns the bytes in
  the plan's totals?

This likely belongs with the #113 unified plan model rather than with any single
provider.

## Acceptance criteria

- [ ] A stated rule for targets nested inside a protected `agent-state` entry.
- [ ] Plan totals do not double-count nested targets.
- [ ] Tests cover both nesting directions.

Found while verifying #148 (leaf L2).

