---
id: AIB-64
title: Add --no-guide and preserve classic clean paths
status: Done
labels:
  - enhancement
  - cli
  - safety
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
completed_date: '2026-07-10'
---
## Description
Part of #62

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

Making guided cleanup the default must not remove the predictable executor path for scripts, power users, and explicit category/tool cleanup commands.

## Scope

- Add `--no-guide` as the explicit opt-out from default guided cleanup.
- Preserve classic cleanup whenever the user supplies explicit cleanup selectors such as `--tool`, `--category`, `--include-active`, or `--risky`.
- Define and implement the routing precedence between `--guide`, `--no-guide`, explicit selectors, `--interactive`, `--dry-run`, `--force`, and `--age`.
- Ensure help text makes the default and opt-out discoverable without overwhelming the command.

## Acceptance Criteria

- [x] `aibris clean --no-guide` uses the classic filter/delete flow.
- [x] Explicit selector commands keep classic behavior unless `--guide` is explicitly supplied.
- [x] Conflicting flags have deterministic behavior and tests.
- [x] `aibris clean --help` documents the guided default and `--no-guide` escape hatch.
- [x] Existing clean tests still pass without weakening safety expectations.

## Completion Evidence

- PR #71 merged as `e67078b`.
- Issue #64 closed as completed.
