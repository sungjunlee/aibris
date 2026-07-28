---
id: AIB-146
title: Replace the absolute scan-time baseline with a same-session delta method
status: To Do
labels:
  - documentation
  - docs
  - scanner
  - type:chore
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-28'
---
## Description
## Goal

Replace the absolute full-home scan wall-clock baseline with a same-session delta method, so provider additions can be judged for performance regression at all.

## Evidence

`docs/DOGFOOD.md` records `19.2s` as the full-home scan baseline from the 2026-07-26 audit. While verifying #138 leaf 1, that number turned out not to be a usable yardstick. Measured on the same machine, same binary, within one hour:

| Condition | `origin/main` |
| --- | ---: |
| cold filesystem cache | 34.93s |
| warm cache, run 1 | 12.68s |
| warm cache, run 2 | 11.24s |

An 11s-to-35s spread on an unchanged binary swamps any plausible provider cost. The executor's own report of `15.43s` for its branch was inside that noise band and could not be reproduced either.

The **delta** against a same-session build of the base branch was stable:

| Condition | base | #138 L1 branch | delta |
| --- | ---: | ---: | ---: |
| cold | 34.93s | 39.11s | +4.18s |
| warm, mean of 2 | 11.96s | 16.13s | +4.17s |

## Why this matters now

Epic #137 adds four to five providers across #138, #139, #140, and #142. `~/.codex/sessions` alone holds 6,711 files. Several of those Done Criteria ask for scan time "against the 19.2s baseline", which cannot distinguish a real regression from cache state — so the checks are currently unfalsifiable, and a genuine regression could land unnoticed.

## Acceptance criteria

- [ ] `docs/DOGFOOD.md` states the delta method and retires `19.2s` as a standalone baseline, keeping it only as a labelled historical observation.
- [ ] The method specifies building the base branch and the change in the same session, alternating runs, and reporting the delta plus the observed cache condition.
- [ ] The method notes that relay run worktrees under `~/.relay/worktrees` change during any relay-driven session, so counts and timings must be captured within one run rather than compared across runs.
- [ ] Remaining #137 child issues that reference the absolute baseline are updated to ask for a delta instead.
- [ ] If a repeatable harness is cheap to add, `make` gains a target that runs the alternating comparison and prints the delta; otherwise the documented manual procedure is sufficient and the decision is recorded.

## Out of scope

- Optimizing scan performance. This issue is about being able to measure it.
- The repeatable full-home performance budgets in #129, which this unblocks but does not replace.

