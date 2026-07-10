---
id: AIB-65
title: Harden non-TTY guided fallback and explicit age routing
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

A better default must remain safe in scripts, CI, piped output, and terminal contexts where interactive prompts cannot be answered.

## Scope

- Detect non-TTY stdin/stdout conditions for default guided cleanup.
- Avoid hanging in non-TTY mode.
- In non-TTY dry-run mode, emit a deterministic textual guided plan.
- In non-TTY non-dry-run mode, require explicit force/selection behavior rather than prompting indefinitely.
- Ensure `--age` continues to constrain default guided recommendations and classic cleanup consistently.

## Acceptance Criteria

- [x] Non-TTY `aibris clean --dry-run` exits after printing a deterministic plan.
- [x] Non-TTY `aibris clean` does not block waiting for input.
- [x] `--age 1d` and `--age 7d` visibly affect guided recommendation totals.
- [x] Tests cover TTY and non-TTY routing behavior.
- [x] Error/help text tells users how to proceed safely when deletion cannot be confirmed.

## Completion Evidence

- PR #71 merged as `e67078b`.
- Issue #65 closed as completed.
