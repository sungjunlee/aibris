---
milestone: Future
status: completed
started: 2026-10-07
due: TBD
scope: ["cmd/**", "internal/**", "tools/perfharness/**", ".github/workflows/**", "Makefile", "install.*", "test/**", "docs/**"]
---

# 2026-10-07 Audit Remediation

## Goal

All sub-issues of the 2026-10-07 engineering audit (Epics #581-#584) are
implemented, independently reviewed, verified, and merged.

## Plan

### Batch 1 (P0, independent)
- [x] #585 Revalidate cached orphaned worktrees at the mutation boundary [PR:#601]
- [x] #586 Duplicate and escaped cwd keys cannot hide a live Claude owner [PR:#601]
- [x] #588 perfharness owns only its run child directory [PR:#601]
- [x] #587 Re-derive cleanup commands from the live catalog [PR:#601]

### Batch 2 (after #587, plus independent P1-P3)
- [x] #589 Refuse deletion on incomplete activity or post-barrier cancellation [PR:#601]
- [x] #594 Every clean confirmation observes cancellation [PR:#601]
- [x] #591 Remove the cubic lookup from unified plan construction [PR:#601]
- [x] #592 Align Codex activity homes, cache identity and metadata reader [PR:#601]
- [x] #595 Exercise real installer failure paths [PR:#601]
- [x] #596 Preserve activity reason codes in final JSON [PR:#601]
- [x] #597 Honor explicit retention roots and keep walking siblings [PR:#601]
- [x] #600 Keep the unregistered Codex tmp implementation, documented [PR:#601]

### Batch 3 (dependents)
- [x] #593 Publish only after same-SHA verification [PR:#601]
- [x] #598 One Size meaning across estimators [PR:#601]
- [x] #590 One domain plan and typed prepared execution [PR:#601]

### Batch 4 (final contracts)
- [x] #599 Separate current contracts from history [PR:#601]

## Running Context

- Each sub-issue was implemented on its own branch, independently reviewed by
  a different model family, and merged into one integration branch with a
  `Merge #N` commit. PR #601 was merged with a merge commit, so each issue
  remains a separate review and rollback unit.
- Execution receipt codes added here (`cleanup_recipe_changed`,
  `worktree_evidence_changed`) are in the #596 supported-code catalog and
  `docs/JSON_SCHEMA.md`.
- `Size` is apparent bytes from one Go walker, with hardlinks counted once
  per (device, inode) within a target on Unix. The pre-merge real-HOME check
  found per-path hardlink counting overstated `node_modules` up to 2x.
- bash 4.4+ reports the trap's triggering status from a bare `return` inside
  an EXIT-trap call chain; installer helpers return `$?` explicitly. macOS
  bash 3.2 hides this, so Linux CI is the authority for installer behavior.
- Follow-ups: #602 (Windows command tests), #603 (guided receipt typed
  identity), #604 (perfharness module coupling), #605 (npm/go cleanup scope),
  #606 (piped prompt input), #607 (root help lists Grok).

## Progress

- 2026-10-07: Admitted all 16 sub-issues as four dependency batches.
- 2026-10-07: All sub-issues implemented, reviewed and integrated; every
  merge passed `make check`, the root race suite and the perfharness suite.
- 2026-10-08: PR #601 opened. First CI run caught an installer temp-dir leak
  on Linux and a missing `.exe` in perfharness on Windows; both fixed.
- 2026-10-08: Pre-merge hermetic E2E (temporary HOME) passed and showed the
  baseline failing exactly the #585, #586 and #594 defects. A read-only
  real-HOME scan and dry-run showed unchanged cleanup decisions and led to
  the per-inode hardlink fix for #598.
- 2026-10-08: PR #601 merged after all checks passed, including native
  Windows. #585-#600 closed. Epics #581, #583 and #584 closed. #582 stays
  open until a real tag exercises the same-SHA release gate. Sprint closed.
