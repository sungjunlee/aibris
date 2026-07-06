---
milestone: Guided Codex Cleanup
status: planned
started: 2026-07-05
due: 2026-07-12
objectives:
  - Make active Codex worktree cleanup reviewable without making it automatic.
  - Default-select only low-risk cleanup candidates that pass conservative git
    and Codex activity checks.
  - Preserve the existing dry-run and confirmation safety model.
component: "clean"
---

# Guided Codex Cleanup

## Goal

Turn the approved guided cleanup design into a safe, usable sprint: fix target
identity first, add Codex activity and git safety signals, then expose
`aibris clean --guide` with low-risk items selected by default and a normal
dry-run preview before deletion.

## Source Of Truth

- GitHub milestone: https://github.com/sungjunlee/aibris/milestone/3
- Epic: #49 `[Epic] Guided Codex worktree cleanup`
- Design spec:
  `docs/superpowers/specs/2026-07-05-guided-codex-worktree-clean-design.md`

## Plan

### Batch 1 - Target Identity Foundation

- [x] #50 Fix cleanup target deduplication and nested overlap accounting
  → PR #56 merged

This comes first because every later recommendation, total, and dry-run preview
depends on path-safe cleanup targets.

### Batch 2 - Independent Signals

- [ ] #51 Add Codex session metadata activity index
- [ ] #52 Add conservative git safety inspection for guided cleanup

These can proceed independently after #50. Both are advisory/protective inputs
to the planner, and both must fail closed.

### Batch 3 - Recommendation Planning

- [ ] #53 Build guided Codex worktree recommendation planner

This combines scan results, deduped targets, Codex activity, project freshness,
size/age thresholds, and git safety into selected vs protected rows.

### Batch 4 - User-Facing Guide

- [ ] #54 Add clean --guide toggle and preview UX

This adds the command mode, default Codex worktree filters, selected/protected
rendering, number toggles, abort, and dry-run preview handoff.

### Batch 5 - Documentation And Dogfood

- [ ] #55 Document and dogfood guided Codex cleanup workflow

This lands after runtime behavior exists so the docs and dogfood transcript
match the actual command output.

## Definition Of Done

- All child issues #50-#55 are closed.
- GitHub milestone `Guided Codex Cleanup` has no open issues.
- `go test ./...` passes.
- `go build ./...` passes.
- `go vet ./...` passes.
- `docs/DOGFOOD.md` includes a real local guided dry-run transcript with no
  deletion.
- `skills/aibris/SKILL.md` routes active Codex worktree bloat to
  `aibris clean --guide` while preserving dry-run-before-delete rules.

## Progress

- 2026-07-05: Created epic #49 and implementation issues #50-#55.
- 2026-07-05: Created GitHub milestone `Guided Codex Cleanup` with due date
  2026-07-12 and assigned #49-#55.
- 2026-07-06: Closed #50 via PR #56. Target deduplication, nested overlap
  accounting, and dry-run audit display now have a stable foundation for
  guided cleanup planning.
