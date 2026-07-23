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
- `Evidence-Based Worktree Reclamation` milestone #5 is complete. It shipped
  v0.8.0 with cleanup-unit identity, ref reachability, per-repository retention,
  unified activity evidence, and Git-aware active removal.
- The completed v0.8.0 evidence initiative is distinct from the current
  milestone #6 `0.8.x Reliability & Trust` patch-readiness work, tracked by
  epic #104 and issues #105-#111. This active sprint closes default-flow,
  selector-validation, exit-status, partial-scan, black-box testing, and
  public-contract gaps before a maintainer release decision.
- Planned follow-on milestones have no due dates: #7 `0.9.x Unified Cleanup
  Experience`, #8 `0.x OSS Distribution & Release Trust`, and #9 `0.x
  Automation & Schema`. Long-horizon configuration and performance work stays
  in the existing `Future` milestone.

## Release Posture

- The project intentionally remains in the 0.x series until the maintainer is
  satisfied with the product experience. Do not create or schedule a v1.0.0
  milestone merely because the current roadmap is complete.
- Milestones describe capability and quality gates, not promised dates. A 0.x
  release is cut only after its behavior is dogfooded and explicitly approved.
