---
id: AIB-62
title: '[Epic] Make clean default to guided decisions'
status: To Do
labels:
  - enhancement
  - ux
  - cli
  - safety
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
---
## Description
## Context

v0.6.0 introduced a guided Codex worktree cleanup planner, but it is hidden behind `aibris clean --guide`. Real users naturally run `aibris clean` or `aibris clean --dry-run`; the default path should be the most useful, safest decision flow.

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

Codex-heavy usage can create enough active worktrees to fill disk before a simple age threshold like 7d is helpful. Cleanup should analyze session/project recency, identify low-risk recommendations, preselect safe candidates, and let the user remove anything they want to keep.

## Scope

- Make plain `aibris clean` and `aibris clean --dry-run` enter guided cleanup when no explicit cleanup filters are supplied and Codex worktree review is valuable.
- Preserve explicit classic cleanup paths for scripted/power usage.
- Add `--no-guide` as the opt-out.
- Keep non-TTY behavior deterministic and non-blocking.
- Ship v0.6.1 for default guided routing.
- Follow with v0.7.0 for the interactive TTY checklist UI.

## Child Issues

### v0.6.1: Default guided routing

- [ ] #63 Auto-enter guided cleanup from default clean
- [ ] #64 Add --no-guide and preserve classic clean paths
- [ ] #65 Harden non-TTY guided fallback and explicit age routing
- [ ] #66 Refresh docs and dogfood around default clean --dry-run
- [ ] #67 Release v0.6.1 default guided clean

Suggested order: #63 first, #64 alongside or immediately after #63, #65 before release, #66 after routing behavior is stable, then #67.

### v0.7.0: Interactive checklist

- [ ] #68 Design TTY checklist renderer for guided clean
- [ ] #69 Implement TTY checklist UI with text fallback
- [ ] #70 Release v0.7.0 guided checklist UI

Suggested order: #68 after the v0.6.1 route exists, #69 after #68, then #70.

## Done Criteria

- [ ] Plain `aibris clean --dry-run` surfaces the guided analysis without requiring users to know `--guide`.
- [ ] Plain `aibris clean` keeps deletion confirmation and low-risk recommendation framing.
- [ ] Explicit classic cleanup flags remain predictable and script-friendly.
- [ ] Non-TTY operation never hangs.
- [ ] Docs, dogfood notes, version bumps, tags, and GitHub releases are complete for v0.6.1 and v0.7.0.

