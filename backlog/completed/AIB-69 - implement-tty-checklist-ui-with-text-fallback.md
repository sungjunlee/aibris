---
id: AIB-69
title: Implement TTY checklist UI with text fallback
status: Done
labels:
  - enhancement
  - ux
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

Guided cleanup should let users act on recommendations directly from the default path, with visible selection control and freed-space feedback.

## Scope

- Implement the guided TTY checklist renderer.
- Preselect low-risk recommended items by default.
- Support deselecting individual items before deletion.
- Show total selected size and update it as selections change.
- Support changing age threshold within the guided flow if the final design confirms it.
- Preserve text fallback and non-TTY behavior.

## Acceptance Criteria

- [x] Guided TTY mode renders a checkbox-style list of candidate deletions.
- [x] Low-risk recommendations are selected by default.
- [x] Selection changes update projected freed-space totals.
- [x] Deletion only executes after explicit final confirmation.
- [x] Text fallback remains usable and covered by tests.

## Completion Evidence

- PR #76 merged as `dd85f64`.
- Non-TTY and pseudo-TTY dogfood passed without deletion.
- Issue #69 closed as completed.
