---
milestone: 0.x Automation & Schema
status: active
started: 2026-08-09
due: TBD
scope: ["cmd/**", "internal/cleaner/**", "README.md", "docs/**"]
---

# JSON Contract Hardening

## Goal

JSON execution follows one explicit route, deletion receipts credit only
attributable mutations, and the public 0.x compatibility boundary is documented.

## Plan

- [~] #202 Make JSON execution route semantics explicit [PR:#204]
- [~] #203 Require mutation-attempt evidence for active-worktree byte credit [PR:#204]
- [~] #127 Document 0.x compatibility and deprecation policy [PR:#204]

## Running Context

- PR #201 shipped current-process JSON receipts. Its final independent review
  identified two non-blocking hardening seams: explicit guided JSON execution
  precedence and active-worktree mutation attribution symmetry.
- Non-dry-run JSON remains a machine contract: stdout is one receipt document,
  paths are opt-in, and unsafe or ambiguous route combinations fail before scan.
- #127 follows the code hardening so the compatibility policy documents the
  actual stable surface rather than an intended one.

## Progress

- 2026-08-09: Closed the completed execution-receipts sprint, created #202 and
  #203 from final review evidence, and admitted them as a parallel first batch
  ahead of #127.
- 2026-08-09: Implemented #202/#203 in parallel, then documented the resulting
  stable surface for #127. PR #204 is under independent exact-head review.
  
