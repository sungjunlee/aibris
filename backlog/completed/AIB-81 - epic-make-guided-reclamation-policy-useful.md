---
id: AIB-81
title: '[Epic] Make guided reclamation policy useful'
status: Done
labels:
  - enhancement
  - ux
  - cli
  - safety
priority: medium
milestone: Evidence-Based Worktree Reclamation
created_date: '2026-07-12'
completed_date: '2026-07-13'
---
## Description
## Context

The v0.7.0 checklist is usable, but the evidence and policy feeding it remain over-conservative and conflate recent activity, retention, and idle age.

PRD: https://github.com/sungjunlee/aibris/blob/codex/worktree-clean-policy-prd/docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md
Planning PR: https://github.com/sungjunlee/aibris/pull/79

## Scope

- unified worktree activity evidence
- 6-hour hard activity lock
- canonical-repository recent-3 retention
- 3-day idle recommendation threshold
- orthogonal age controls and useful protected-only routing
- dogfood, documentation, and v0.8.0 release

## Done Criteria

- [x] Every child issue is closed by merged work.
- [x] Missing upstream alone never locks a row.
- [x] Recent, retained, and recommended states have deterministic reasons.
- [x] Preserved-baseline dogfood recommends at least 10 GB while preserving hard locks; the live sandbox result is recorded separately and truthfully.
- [x] v0.8.0 is published and installer-smoked.

## Child Issues

- [x] #85 Add unified worktree activity evidence
- [x] #87 Implement hierarchical retention and recommendation policy
- [x] #88 Reclassify guided rows and orthogonalize age controls
- [x] #89 Route protected-only Codex pressure to guided review
- [x] #90 Dogfood Git-aware reclamation and refresh documentation
- [x] #91 Release v0.8.0 evidence-based worktree reclamation

## Completion Evidence

- PRs #95, #96, #99, #98, #100, and #101 merged; all child issues closed.
- Preserved baseline recommends 13.5 GB with hard-lock and multi-member fixtures.
- GitHub Release v0.8.0 published with six platform archives and checksums.
- Temporary-prefix installer smoke reported `aibris version 0.8.0`.
