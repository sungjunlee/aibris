---
milestone: Scan + Clean Safety
status: active
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

- [~] #21 AIB-1 Add explicit scan roots and root validation → PR #27 (reviewing)

### Batch 2 — Discovery Coverage

- [~] #22 AIB-2 Expand node_modules discovery to home-scoped roots → PR #27 (reviewing)

### Batch 3 — Cleanup Safety

- [~] #23 AIB-3 Exclude active worktrees from cleanup by default → PR #27 (reviewing)

### Batch 4 — Agent-Facing Output

- [~] #24 AIB-4 Add derived status, risk, and reason fields to scan JSON → PR #27 (reviewing)

### Batch 5 — Docs and Skill Workflow

- [~] #25 AIB-5 Document home-scoped scanning and active worktree protection → PR #27 (reviewing)

### Later — Cache Cleanup

- [~] #26 AIB-6 Prefer official cache cleanup commands where safe → PR #27 (reviewing)

## Running Context

- Source plan: `docs/SCAN_CLEAN_IMPROVEMENT_PLAN.md`.
- Implementation should be sequential. The scanner contract touches shared
  interfaces, command flags, adapters, and tests.
- Do not change cache cleanup execution semantics in the first PR.

## Progress

- 2026-06-01: Derived implementation issues from the reviewed improvement plan.
- 2026-06-01: Created GitHub Issues #21-#26 and linked local task files.
- 2026-06-01: Implemented scan roots, home-scoped node_modules discovery, active worktree protection, JSON decision fields, command-backed cache cleanup, docs, and tests on `codex/scan-clean-safety`.
