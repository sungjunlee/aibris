---
id: AIB-137
title: '[Epic] Cover the agent state store surface'
status: Done
labels:
  - enhancement
  - ux
  - cli
  - scanner
  - safety
  - type:feature
priority: high
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
completed_date: '2026-08-06'
---
## Description
## Goal

Close the coverage gap measured in the 2026-07-26 audit (`docs/DOGFOOD.md`):
about 15% of the agent-produced debris surface is discovered, while ~16.6 GB of
generic build debris that general-purpose cleaners already handle is fully
covered. The gap is provider coverage, not policy or rendering.

## Children

- [x] AIB-138 provable-orphan cleanup — merged (L1–L3, PRs #144/#148/#154)
- [x] AIB-139 session, transcript, and run-manifest providers — merged,
      re-scoped 2026-08-06 to a read-only codex-sessions inventory (PR #187);
      execution layer parked
- [x] AIB-140 container-registry worktree discovery — merged (PR #163)
- [~] AIB-141 tool-agnostic guided review — parked (branch at `0a47323`,
      pre-publication parked on hardened advisory adapter availability)
- [~] AIB-142 byproduct providers — L1 merged (PR #164); L2/L3 blocked
      upstream on producer-documented layouts + cooperative exclusion
- [x] AIB-143 documented product boundary — merged (PR #188)

## Acceptance criteria

- [x] All actionable children are merged; AIB-141 parked and AIB-142 L2/L3
      blocked upstream with their own trackers (see GitHub issue #137 scope
      adjustment).
- [ ] A re-run of the DOGFOOD coverage audit shows agent-surface coverage
      above 90% — deferred until the parked/blocked providers ship.
- [x] Installed content remains excluded from debris.
- [x] Transcripts are never reclaimed by default (read-only inventory;
      execution layer parked).
- [x] Public docs state the product boundary honestly.
