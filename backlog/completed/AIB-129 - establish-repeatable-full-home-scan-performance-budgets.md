---
id: AIB-129
title: Establish repeatable full-home scan performance budgets
status: To Do
labels:
  - devops
  - scanner
  - type:chore
priority: medium
milestone: Future
created_date: '2026-07-22'
---
## Description
## Goal

Protect the current fast first-value experience as discovery grows.

## Acceptance criteria

- [ ] A deterministic synthetic-home benchmark covers worktrees, node_modules, caches, and noisy pruned directories.
- [ ] Provider and end-to-end baselines are recorded for macOS and Linux where practical.
- [ ] A regression threshold is defined without making CI flaky.
- [ ] Real-home dogfood records sanitized elapsed time and scale separately from CI benchmarks.
- [ ] Performance work never weakens path or worktree validation.
