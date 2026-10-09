# aibris Roadmap

`aibris` will remain in the 0.x series until the maintainer is satisfied with
the product experience. Completing a milestone does not imply a v1.0.0 target,
date, or compatibility promise.

Milestones are capability and quality gates rather than schedules. Releases
are cut only after the relevant behavior is dogfooded and explicitly approved.

## Current unreleased work

As of 2026-10-09, the latest tagged release is v0.16.0 (2026-10-09), which
shows a Go build cache rejected by its layout check, with the reason, instead
of omitting it. No changes are integrated after that tag yet. Tagged history is
recorded in [CHANGELOG.md](../CHANGELOG.md); current behavior is specified in
[SPEC.md](SPEC.md), [JSON_SCHEMA.md](JSON_SCHEMA.md), [INSTALL.md](INSTALL.md),
and [WINDOWS.md](WINDOWS.md).

Next 0.x work:

- Codex activity follow-up: evidence through relocated session stores, and
  Windows junctions in a parent of the Codex home (#634).
- Coverage for more AI agent stores, each backed by an orphan proof or a
  rebuildable-cache contract (`docs/CATEGORY.md`).
- Separate the owning tool from the provider in `--tool` and JSON
  (`owner`, `target_id`) under the 0.x deprecation policy.

## Shipped

### 0.16.0 Go Cache Diagnostics

Published 2026-10-09. A Go build cache rejected by its layout check appears in
scan and dry-run with its size and cause (`go_cache_unverified`) and is never
selectable.

### 0.15.2 Resumed Session Activity

Published 2026-10-09. Work appended to an existing Codex session counts as
activity in guided review (recent-activity lock, minimum idle age, retention
order) and at the pre-removal check, and a Codex home that is a Windows
junction fails closed.

### 0.15.1 Activity Evidence Fixes

Published 2026-10-09. Guided review refreshes Codex session activity and
rechecks it before each removal step, relocated or unreadable session stores keep
active worktrees locked, Orca-only users can review active Orca worktrees, and
Windows junction cache leaves are refused.

### 0.15.0 Orca Support

Published 2026-10-09. Orca-managed worktrees discovered as independent
owners, Orca's macOS Codex home as a resolved home for worktrees, activity,
retention and logs, Orca activity checked across every Codex home with fail-closed coverage,
and retention across all resolved homes.

### 0.14.1 Cleanup Scope Fixes

Published 2026-10-08. Go and npm caches removed through the deletion gate
without running their tools, a Go-only layout check that keeps shared
directories safe, previews that match execution for symlinked caches, one
stdin reader per clean run, and guided receipts that keep their evidence.

### 0.14.0 Audit Remediation

Published 2026-10-08. Re-verified worktree, agent-state, and activity evidence
at the mutation boundary, catalog-derived cleanup commands with cache-only
Homebrew removal, cancellable confirmations, one apparent-byte `size`, a single
typed execution plan, and publication gated on verification of the tagged
commit.

### 0.13.0 Safety Gate and Coverage

Published 2026-10-06. One deletion gate for every removal, Grok CLI session
stores, an env-aware cache catalog with the npx cache, concurrent Git
evidence for guided review, and a quieter, aligned scan summary.

### 0.12.0 Reclaim Accuracy

Published 2026-08-30. Safer guided uniqueness, worktree leftover/sidecar
classification, scan reclaim headline, live GOCACHE / explicit `--root`,
observed command `freed_bytes`, and `post_clean` receipt fields.

### 0.11.0 Protected-Weight Reclamation

Published after the 2026-08-17 dogfood pass:

- all-tools guided worktree review (`--guide` no longer implies `--tool codex`)
- `clean --strip` for regenerable subtrees in protected worktrees
- home-volume pressure on scan and optional `--pressure` cache-age relaxation
- registered two-level worktree members, opt-in APFS snapshot thinning, and
  expanded official caches

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

Historical status as of 2026-08-09: the then-unreleased work included
`--exclude`, completions/man pages, explicit temp roots, `--diagnostics`,
README onboarding, Codex home overrides, receipts, agent-state grace, in-tree
activity, and #141. These subsequently shipped in the 0.10.x/0.11.x series;
see the dated [changelog](../CHANGELOG.md). This is not the current backlog.

### 0.9.0 Unified Cleanup Experience

One plan, one mixed-category review, one confirmation and receipt contract.

### 0.8.x Reliability & Trust

Selector, execution-failure, and partial-scan outcomes made unambiguous;
CLI contracts locked with compiled-process tests.

## Completed distribution and automation tracks

### OSS Distribution & Release Trust

- packaged completions and manual pages (shipped, #119)
- verified Homebrew installation (shipped, #118)
- an explicit experimental Windows support contract (shipped, #120); native
  PowerShell installation shipped in v0.12.2
- SBOM and artifact provenance (shipped in v0.12.0, #121)
- curated release notes and public link checks (shipped, #122)

### Automation & Schema

Complete and closed 2026-08-17. Shipped a versioned scan JSON schema,
machine-readable clean plans and receipts, provider diagnostics, and
`docs/COMPATIBILITY.md`.

## Future

Additional proof-backed AI stores and separation of owning tool from provider
(`owner`, `target_id`) remain proposals under the 0.x compatibility policy.
Further narrowing of name-based cleanup checks and alignment of clean-plan
output with the scan summary also remain proposals; live catalog command
authorization is already implemented in the unreleased work above.
Repeatable full-home performance budgets, parked retention execution, and
further session-store providers remain future work. Exclusions and project
ignore rules already shipped (#128). Priorities may change based on
dogfooding and user feedback; none of these tracks schedules v1.0.0.
