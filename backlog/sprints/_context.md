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
- Milestone #6 `0.8.x Reliability & Trust` completed its implementation scope:
  default-flow visibility, selector validation, truthful exit status,
  partial-scan semantics, black-box contracts, and public trust surfaces.
  Release gate #111 remains open and explicitly deferred pending maintainer
  approval, so milestone #6 stays open without a tag or promised date.
- The next active product initiative is milestone #7 `0.9.x Unified Cleanup
  Experience`: replace the interim guided-then-classic handoff with one
  inspectable cleanup plan, mixed-category review, and a single confirmation
  and execution contract.
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
- Open release-gate issues may outlive their implementation sprint. Never mark
  one complete merely to close a sprint; carry it as explicitly deferred work.
