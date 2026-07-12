---
id: AIB-82
title: Model physical cleanup units and nested Git members
status: To Do
labels:
  - enhancement
  - scanner
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
---
## Description
## Parent

- Epic: #80
- Milestone: Evidence-Based Worktree Reclamation
- PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md

## Dependencies

None

## Scope

- Group scanner rows by canonical physical target path.
- Enumerate direct and nested Git members without discarding duplicates.
- Count size once per cleanup unit while preserving every member.
- Keep the public scan JSON schema compatible.

## Acceptance Criteria

- [ ] Direct, nested, duplicate, and two-member fixtures produce deterministic units.
- [ ] A two-member target yields one physical target and two safety members.
- [ ] Context cancellation and bounded discovery remain enforced.
- [ ] Existing scan JSON compatibility tests pass.
