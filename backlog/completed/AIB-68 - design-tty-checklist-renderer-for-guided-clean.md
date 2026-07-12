---
id: AIB-68
title: Design TTY checklist renderer for guided clean
status: Done
labels:
  - enhancement
  - ux
  - cli
priority: medium
milestone: Default Guided Clean
created_date: '2026-07-09'
updated_date: '2026-07-09 22:56'
completed_date: '2026-07-10'
---
## Description
Part of #62

PRD: https://github.com/sungjunlee/aibris/blob/main/docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md

## Problem

The next product step after default guided routing is a compact terminal selection experience where low-risk recommendations are preselected and users remove items they want to keep.

## Scope

- Design the TTY checklist interaction model for guided cleanup.
- Define default selection rules for low-risk recommendations.
- Define controls for toggling items, changing age threshold, reviewing projected freed space, and confirming deletion.
- Specify fallback text behavior for terminals that cannot render the interactive UI.
- Keep the design implementable within the existing Cobra CLI and cleaner flow.

## Acceptance Criteria

- [x] A short design note or issue comment defines the checklist interaction and key bindings.
- [x] Low-risk items are selected by default; users can deselect before deletion.
- [x] Age threshold changes update projected deletion totals in the design.
- [x] The design includes empty, all-active, all-risky, and non-TTY states.
- [x] Implementation risks and dependencies are identified before coding begins.

## Completion Evidence

- PR #72 merged as `ad0400f`.
- `docs/GUIDED_CLEAN_TTY_CHECKLIST.md` landed.
- Issue #68 closed as completed.
