---
id: AIB-137
title: '[Epic] Cover the agent state store surface'
status: To Do
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
---
## Description
## Goal

Close the coverage gap measured in the 2026-07-26 audit (`docs/DOGFOOD.md`):
about 15% of the agent-produced debris surface is discovered, while ~16.6 GB of
generic build debris that general-purpose cleaners already handle is fully
covered. The gap is provider coverage, not policy or rendering.

## Children

- AIB-138 provable-orphan cleanup (do first; validates the AIB-113 plan model)
- AIB-139 session, transcript, and run-manifest providers
- AIB-140 container-registry worktree discovery
- AIB-141 tool-agnostic guided review
- AIB-142 byproduct providers
- AIB-143 documented product boundary

## Acceptance criteria

- [ ] All children are merged.
- [ ] A re-run of the DOGFOOD coverage audit shows agent-surface coverage above 90%.
- [ ] Installed content remains excluded from debris.
- [ ] Transcripts are never reclaimed by default.
- [ ] Public docs state the product boundary honestly.
