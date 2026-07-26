# Backlog Reassessment — 2026-07-26

## Evidence

- A read-only coverage audit of one real developer `$HOME` against a build of
  `main` measured agent state stores at **≈ 18.7 GB actual, 2.7 GB discovered
  (about 15%)**. Scoped to one tool, `scan --root ~/.codex` reported 2.81 GB
  against 16 GB actual. `~/.codex/sessions` alone is 11 GB and is invisible.
  Full evidence is in `docs/DOGFOOD.md`.
- The same scan fully covers ~16.6 GB of generic build debris — `node_modules`
  7.1 GB, Gradle 5.0 GB, uv 4.5 GB — that general-purpose cleaners handle.
- Default `clean --dry-run` planned **1.4 GB, every target `node_modules`**.
  Every agent-specific category contributed 0 B. Guided review reported
  `selected 0 items 0 B` with all rows locked or reviewable.
- Two structural causes are separable from the coverage gap. A global cache
  directory's mtime tracks continuous use, so it can never satisfy `--age 7d`;
  a transcript's mtime is a fixed session end time, so age is meaningful there.
  And guided review is codex-only, so the largest worktree found — a `claude`
  unit, 1.5 GB, idle 93 days — has no default path to reclamation.
- Session stores key directories by working directory with a lossy encoding:
  `/`, `.`, and `_` all collapse to `-`. Decoding names back to paths produced
  false positives during the audit. Every store records an authoritative cwd
  internally, which is the only sound basis for orphan detection.

## Assessment

The 2026-07-23 reassessment justified milestone #7 on structural evidence — two
plans, two confirmations, two receipts — and that reading still holds. What it
did not measure was outcome evidence: how many bytes the default path actually
reclaims in its own subject area. The answer is zero.

This does not invalidate the #113 plan model, which looks sound. It does change
the sequencing risk for #115. Three to four new categories are queued behind
#137, and freezing the execution and confirmation contract against the current
guided/classic pair invites reopening it once those land.

Scope, not policy, is the binding constraint. aibris covers most of a generic
cleaner's territory and about 15% of its own.

## Decision

- Open milestone #10, `0.10.x Agent State Store Coverage`, with epic #137 and
  children #138-#143. Record the audit in `docs/DOGFOOD.md` as the standing
  coverage baseline.
- Land #138 (provable-orphan) before finalizing #115. It is the cheapest new
  category and the strongest test of whether the #113 model absorbs a category
  with no age gate and a proof-based rather than policy-based safety argument.
- Keep #115, #116, #112, and the #117 release gate in milestone #7 unchanged
  otherwise. #116 dogfood should reuse this audit's method.
- Deprioritize the global-cache age exemption. It would unlock ~9.5 GB, but that
  is a general-purpose cleaner's territory and not this project's purpose.
- Treat transcripts as user content in every subsequent decision. Surfacing them
  is in scope; reclaiming them by default is not.
- The project remains in 0.x. Milestone #10 has no due date and implies no
  v1.0.0 schedule.
