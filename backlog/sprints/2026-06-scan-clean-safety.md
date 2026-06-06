---
milestone: Scan + Clean Safety
status: completed
started: 2026-06-01
due: 2026-06-07
objectives: []
component: "scanner"
---

# Scan + Clean Safety

## Goal

Make `aibris scan` accurately inventory development debris under `$HOME`, and
make `aibris clean` protect active worktrees by default.

## Plan

### Batch 1 — Scanner Contract and Roots

- [x] #21 AIB-1 Add explicit scan roots and root validation → PR #27 (merged)

### Batch 2 — Discovery Coverage

- [x] #22 AIB-2 Expand node_modules discovery to home-scoped roots → PR #27 (merged)

### Batch 3 — Cleanup Safety

- [x] #23 AIB-3 Exclude active worktrees from cleanup by default → PR #27 (merged)

### Batch 4 — Agent-Facing Output

- [x] #24 AIB-4 Add derived status, risk, and reason fields to scan JSON → PR #27 (merged)

### Batch 5 — Docs and Skill Workflow

- [x] #25 AIB-5 Document home-scoped scanning and active worktree protection → PR #27 (merged)

### Later — Cache Cleanup

- [x] #26 AIB-6 Prefer official cache cleanup commands where safe → PR #27 (merged)

### Follow-up — Scan and Clean UX

- [x] #28 Improve scan UX with lightweight progress and modern summary output → PR #30 (merged)

## Running Context

- Source plan: `docs/SCAN_CLEAN_IMPROVEMENT_PLAN.md`.
- Implementation landed through PR #27 and PR #30. Future work should start
  from release feedback rather than this sprint plan.

## Progress

- 2026-06-01: Derived implementation issues from the reviewed improvement plan.
- 2026-06-01: Created GitHub Issues #21-#26 and linked local task files.
- 2026-06-01: Implemented scan roots, home-scoped node_modules discovery, active worktree protection, JSON decision fields, command-backed cache cleanup, docs, and tests on `codex/scan-clean-safety`.
- 2026-06-01: PR #27 merged and closed #21-#26.
- 2026-06-06: PR #30 merged scan provider parallelism, terminal spinner progress, clean target plans, and node_modules safety-path cleanup fix.
