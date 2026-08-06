# aibris Roadmap

`aibris` will remain in the 0.x series until the maintainer is satisfied with
the product experience. Completing a milestone does not imply a v1.0.0 target,
date, or compatibility promise.

Milestones are capability and quality gates rather than schedules. Releases
are cut only after the relevant behavior is dogfooded and explicitly approved.

## Current: 0.8.x Reliability & Trust

- make default cleanup show all relevant categories
- make selector, execution-failure, and partial-scan outcomes unambiguous
- lock user-visible CLI contracts with compiled-process tests
- keep security and community documentation aligned with shipped behavior

The milestone may produce a 0.8.x patch release, but it has no promised date.

## Next: 0.9.x Unified Cleanup Experience

- represent guided and classic cleanup candidates in one plan
- review mixed categories through one selection experience
- execute one normalized selection with one receipt and confirmation contract
- dogfood the complete journey before any release decision

## Then: 0.10.x Agent State Store Coverage

The 2026-07-26 coverage audit in `docs/DOGFOOD.md` measured one real developer
home and found aibris discovering about 15% of the agent-produced debris surface
while fully covering generic build debris that general-purpose cleaners already
handle. The gap is provider coverage, not policy or rendering.

The milestone is substantially covered. Shipped since the audit:

- proof-based orphan cleanup for Claude and Cursor recorded-cwd project stores
  (#138) — the first no-age-gate category
- worktree container coverage from any agent tool via the finite exact registry
  plus the bounded `$HOME` convention fallback (#140)
- store-nature classification for the uncovered byproduct stores (#142 L1)
- a read-only `codex-sessions` retention inventory with UTC-month buckets and
  orphan statistics (#139, re-scoped 2026-08-06; execution layer parked)

Most remaining work is either parked with the execution layer or blocked on
upstream producer cooperation:

- inventory session, transcript, and run-manifest stores beyond codex-sessions
  (cursor chats, relay runs, gstack projects) — future provider leaves under
  the same root/unit/timestamp discipline
- retention execution (selector, manifest, executor) stays parked per #139
  re-scope
- cover agent byproduct stores (#142 L2/L3; blocked on producer-documented
  versioned layouts and cooperative exclusion protocols)

Locally implementable leaves remain:

- find worktree containers regardless of nesting depth and member layout
- admit worktree units from any agent tool into guided review

Transcripts are user content. Surfacing them is in scope; reclaiming them by
default is not.

## Parallel 0.x Tracks

### OSS Distribution & Release Trust

- verified Homebrew installation
- packaged completions and manual pages
- an explicit Windows support contract
- SBOM and artifact provenance
- curated release notes and public link checks

### Automation & Schema

- a versioned scan JSON schema
- machine-readable clean plans and receipts
- provider timing and diagnostics
- a documented 0.x compatibility and deprecation policy

## Future

Configuration, exclusions, ignore rules, and repeatable full-home performance
budgets remain future work. Priorities may change based on dogfooding and user
feedback; none of these tracks schedules v1.0.0.
