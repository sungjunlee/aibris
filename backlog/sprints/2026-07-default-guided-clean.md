---
milestone: Default Guided Clean
status: active
started: 2026-07-09
due: 2026-07-16
objectives:
  - Make plain clean and clean --dry-run surface guided Codex worktree decisions by default when that is the valuable path.
  - Preserve classic clean behavior for explicit filters, scripts, non-TTY contexts, and opt-out usage.
  - Ship the default guided behavior as v0.6.1, then follow with a compact TTY checklist release in v0.7.0.
component: "clean"
---

# Default Guided Clean

## Goal

Make `aibris clean` the pleasing guided path for Codex worktree bloat without weakening the classic executor, confirmation, or non-TTY safety model.

## Source Of Truth

- GitHub milestone: https://github.com/sungjunlee/aibris/milestone/4
- Epic: #62 `[Epic] Make clean default to guided decisions`
- PRD: `docs/superpowers/specs/2026-07-09-clean-default-guided-prd.md`

## Plan

### Batch 1 - v0.6.1 Runtime Route

- [~] #63 Auto-enter guided cleanup from default clean -> relay run issue-63-20260709130417141-761d43bf
- [~] #64 Add --no-guide and preserve classic clean paths -> relay run issue-63-20260709130417141-761d43bf
- [~] #65 Harden non-TTY guided fallback and explicit age routing -> relay run issue-63-20260709130417141-761d43bf

### Batch 2 - v0.6.1 Docs And Dogfood

- [ ] #66 Refresh docs and dogfood around default clean --dry-run

### Batch 3 - v0.6.1 Release

- [ ] #67 Release v0.6.1 default guided clean

### Batch 4 - v0.7.0 Checklist Follow-Up

- [ ] #68 Design TTY checklist renderer for guided clean
- [ ] #69 Implement TTY checklist UI with text fallback
- [ ] #70 Release v0.7.0 guided checklist UI

## Definition Of Done

- All child issues #63-#70 are either closed by merged work or explicitly left as scoped follow-up with a clean issue comment.
- `aibris clean --dry-run` enters guided Codex cleanup by default only when no explicit classic cleanup filters are supplied and guided review has useful candidates.
- `aibris clean --no-guide` and explicit filter commands keep the classic clean path.
- Non-TTY clean never hangs and dry-run remains delete-free.
- `go test ./...`, `go build ./...`, and `go vet ./...` pass before release commits/tags.
- README, SPEC, DOGFOOD, and `skills/aibris/SKILL.md` match the default-guided product stance.

## Running Context

- Prior sprint #49-#55 built `clean --guide`: selected/protected rows, number toggles, low-risk planner, Codex session activity, git safety checks, and dry-run preview handoff.
- Existing project context says the CLI should stay a conservative scanner/executor. This sprint narrows that rule: plain no-filter `clean` may choose the guided Codex worktree decision path, while explicit cleanup filters and `--no-guide` remain the classic executor path.
- #63-#65 touch the same `clean` command route, so run them as one implementation wave with one review anchor rather than three conflicting parallel edits.
- #66 lands after runtime output is stable so examples and dogfood match the actual command.
- #67 releases only after runtime, tests, docs, and dogfood verification are complete.
- Batch 1 should avoid broad TTY UI work. v0.6.1 may reuse the current textual guide; v0.7.0 owns the richer checklist polish.
- #68-#70 are intentionally after v0.6.1: default routing should ship before introducing a richer terminal renderer.

## Progress

- 2026-07-09: Created active sprint for epic #62 and split work into v0.6.1 runtime/docs/release followed by v0.7.0 checklist follow-up.
- 2026-07-09: Dispatched Batch 1 (#63-#65) through relay-ready/relay-plan as `default-guided-runtime`, run `issue-63-20260709130417141-761d43bf`.
