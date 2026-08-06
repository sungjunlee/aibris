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

- classify session-store entries whose recorded working directory is gone
- inventory session, transcript, and run-manifest stores as read-only UTC-month
  retention aggregates (codex-sessions shipped); the retention execution layer
  (selector, manifest, executor) stays parked per #139 re-scope
- find worktree containers regardless of nesting depth and member layout
- admit worktree units from any agent tool into guided review
- cover agent byproduct stores
- state the product boundary honestly: complement general-purpose cleaners
  rather than compete with them

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
