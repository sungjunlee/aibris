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
