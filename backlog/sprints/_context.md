# Project Context

## Architecture Decisions

- `aibris` remains a conservative scanner/executor, while plain no-filter
  `clean` may choose the guided Codex decision path. Explicit cleanup selectors
  and `--no-guide` preserve the classic executor path.
- `active` worktree status is structural linked health, not recent liveness.
  Automatic recommendations must combine Git recoverability, activity,
  retention, age, and size rather than age-only filtering.
- Guided cleanup uses one renderer-independent recommended/reviewable/locked
  selection model. Hard-safety rows remain locked even when `--force` skips the
  final confirmation.
- `$HOME` scan coverage is the product promise, but it must prune high-noise
  personal/system directories and reject roots outside `$HOME`.

## Known Follow-Ups

- `Guided Codex Cleanup` milestone #3 and `Default Guided Clean` milestone #4
  are complete. They shipped the activity index, conservative planner, default
  guided route, and v0.7.0 checklist model.
- The proposed next initiative is `Evidence-Based Worktree Reclamation`, using
  `docs/superpowers/specs/2026-07-10-evidence-based-worktree-reclamation-prd.md`
  as the planning source of truth for cleanup units, ref reachability,
  per-repository retention, and Git-aware active removal.
