# aibris Roadmap

`aibris` will remain in the 0.x series until the maintainer is satisfied with
the product experience. Completing a milestone does not imply a v1.0.0 target,
date, or compatibility promise.

Milestones are capability and quality gates rather than schedules. Releases
are cut only after the relevant behavior is dogfooded and explicitly approved.

## Current: 0.11.x Protected-Weight Reclamation

Implementation for this milestone is on `main` and unreleased. The remaining
item is a decision, not more code:

- admit worktree units from any agent tool into guided review (#141, #228)
- strip regenerable subtrees from protected worktrees (`clean --strip`,
  #221 / #226, with the cwd barrier from #227)
- decide whether `node_modules` and `ai-logs` should follow the in-tree
  activity signal (#218)

`--guide` no longer implying `--tool codex` is a documented 0.x default
change. The next tag should be v0.11.0 after dogfood, with upgrade notes.

## Shipped

### 0.10.0 / 0.10.x Agent State Store Coverage

Published 2026-08-09. Actionable provider coverage shipped; remaining leaves
are parked or blocked:

- proof-based orphan cleanup for Claude and Cursor recorded-cwd project stores
  (#138)
- worktree container coverage via the finite exact registry plus the bounded
  `$HOME` convention fallback (#140)
- store-nature classification for uncovered byproduct stores (#142 L1)
- a read-only `codex-sessions` retention inventory (#139; execution parked)
- #142 L2/L3 stay blocked on producer-documented layouts and fencing
- session / transcript / run-manifest stores beyond Codex stay future work

Unreleased on `main` since the tag: `--exclude`, packaged completions and man
pages, explicit system-temp-dir roots, `--diagnostics`, README first-cleanup
restructure, `CODEX_HOME` / `AIBRIS_CODEX_HOMES`, `--receipt-file`,
`--agent-state-grace`, in-tree cache activity, and #141.

### 0.9.0 Unified Cleanup Experience

One plan, one mixed-category review, one confirmation and receipt contract.

### 0.8.x Reliability & Trust

Selector, execution-failure, and partial-scan outcomes made unambiguous;
CLI contracts locked with compiled-process tests.

## Parallel 0.x Tracks

### OSS Distribution & Release Trust

- packaged completions and manual pages (shipped, #119)
- verified Homebrew installation (#118)
- an explicit Windows support contract (#120)
- SBOM and artifact provenance (#121)
- curated release notes and public link checks (#122)

### Automation & Schema

Complete and closed 2026-08-17. Shipped a versioned scan JSON schema,
machine-readable clean plans and receipts, provider diagnostics, and
`docs/COMPATIBILITY.md`.

## Future

Repeatable full-home performance budgets, parked retention execution, and
further session-store providers remain future work. Exclusions and project
ignore rules already shipped (#128). Priorities may change based on
dogfooding and user feedback; none of these tracks schedules v1.0.0.
