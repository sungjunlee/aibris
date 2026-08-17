---
id: AIB-147
title: 'Make scan''s default-clean figure account for target normalization'
status: To Do
labels:
  - bug
  - cli
  - scanner
  - type:bug
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-28'
---
## Description
## Goal

Make `scan`'s advertised `default clean` figure account for the same target normalization that `clean` applies, so the two commands cannot disagree on nested or duplicate paths.

## Evidence

Found during review of #138 leaf 1. `summarizeCleanup` in `cmd/scan.go` sums every item that passes the eligibility decision. `clean` then additionally applies `filterExistingTargets`, `normalizeCleanTargets`, and active-worktree Git safety before planning.

So an eligible item nested inside another eligible item — for example a `node_modules` directory inside an orphaned worktree — is counted twice by `scan` and collapsed to the parent by `clean`.

This is **pre-existing**, not introduced by #138. `summarizeCleanup` has never applied normalization; the same shape exists on `main` before the `agent-state` category was added. #138 leaf 1 fixed the *eligibility* half of the divergence by making `Filter`, the cleanup audit, and `summarizeCleanup` share one `EvaluateEligibility` decision. Normalization parity is the remaining half.

Not currently reproducible on the maintainer's home: every discovered worktree is `active` and therefore protected, so no eligible-parent-with-eligible-child pair exists. The divergence is latent until an orphaned worktree containing its own cleanable debris appears — which is exactly the case aibris exists to clean.

## Why it belongs here rather than in #138

Normalization is owned by the `UnifiedCleanupPlan` model from #113, and wiring `clean` through that model is #115. Teaching `summarizeCleanup` to normalize independently would add a third implementation of logic that #115 is about to centralize.

## Acceptance criteria

- [ ] `scan`'s `default clean` figure equals what `clean --dry-run` plans for the same roots and options, including when eligible targets nest.
- [ ] The normalization used by `scan` is the same code path `clean` uses, not a parallel implementation.
- [ ] A test covers an eligible child nested inside an eligible parent and asserts both commands report one target and one size.
- [ ] A test covers a target whose path disappeared between scan and clean, and asserts the two remain consistent about it.
- [ ] If `default clean` cannot be made exact without running the full plan pipeline, it is relabelled in the output as an estimate and the docs say so, rather than silently over-promising.

## Out of scope

- The eligibility decision itself, which #138 leaf 1 already consolidated.
- Rewiring `clean` through `UnifiedCleanupPlan`, which is #115. This issue may land after it and reuse it.

