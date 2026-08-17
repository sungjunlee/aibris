# Project Context

## Architecture Decisions

- `aibris` remains a conservative scanner/executor, while plain no-filter
  `clean` may choose the guided worktree review path. Explicit cleanup
  selectors and `--no-guide` preserve the classic executor path.
- `active` worktree status is structural linked health, not recent liveness.
  Automatic recommendations must combine Git recoverability, activity,
  retention, age, and size rather than age-only filtering.
- Guided cleanup uses one renderer-independent recommended/reviewable/locked
  selection model. Hard-safety rows remain locked even when `--force` skips the
  final confirmation.
- `$HOME` scan coverage is the product promise, but it must prune high-noise
  personal/system directories and reject roots outside `$HOME`.
- Guided review admits every tool's active worktrees. A registered
  session-activity reader exists only for Codex; other tools are reviewable
  with `activity_source_not_registered` and are never auto-recommended. The
  6-hour recent-activity hard lock is tool-independent.

## Known Follow-Ups

- `Guided Codex Cleanup` milestone #3 and `Default Guided Clean` milestone #4
  are complete. They shipped the activity index, conservative planner, default
  guided route, and v0.7.0 checklist model.
- `Evidence-Based Worktree Reclamation` milestone #5 is complete. It shipped
  v0.8.0 with cleanup-unit identity, ref reachability, per-repository retention,
  unified activity evidence, and Git-aware active removal.
- v0.9.0 shipped the unified cleanup experience. PR #198 added the versioned,
  path-redacted `clean --dry-run --json` plan contract, and PR #201 completed
  #125 with current-process execution receipts and exit/status agreement.
- PR #204 made JSON execution unambiguously classic, rejects execution-time
  `--guide` before scan, requires mutation-attempt evidence for active-worktree
  byte credit, and established `docs/COMPATIBILITY.md` as the 0.x contract.
- v0.10.0 published the versioned cleanup plan/receipt contract with six
  platform archives, checksums, install smoke, and path-free published-binary
  dogfood recorded in `docs/DOGFOOD.md`.
- The 0.10.x agent-state coverage sprint closed with its actionable scope
  shipped. #142 L2/L3 remain externally blocked on producer identity and
  all-writer fencing; do not manufacture a local cleanup proof.
- #141 shipped on main via PR #228 (2026-08-17). `--guide` no longer implies
  `--tool codex`. The parked branch `issue-141-guided-review-all-worktree-tools`
  at `0a47323` is obsolete. The next release must note the documented default
  change.
- #221 shipped on main via PR #226 (2026-08-17), including the #227 cwd
  barrier. `clean --strip` is a third disposition beside protect and delete.
- The post-v0.11.0 reclaim UX sprint is active (`cmd/**`). Batch 1 shipped
  (#253/#254/#256 via PRs #260/#261/#259). Batch 2 shipped (#255/#258 via
  PRs #263/#264). Remaining: Batch 3 #257.
  #218 is a decision, not an implementation, about whether `node_modules`
  and `ai-logs` follow in-tree activity. Do not start that change without
  an explicit call.
- Milestone #9 `0.x Automation & Schema` is closed. Milestone #8 `0.x OSS
  Distribution & Release Trust` remains open (#118, #120, #121, #122).
  Long-horizon work stays in the existing `Future` milestone.

## Release Posture

- The project intentionally remains in the 0.x series until the maintainer is
  satisfied with the product experience. Do not create or schedule a v1.0.0
  milestone merely because the current roadmap is complete.
- Milestones describe capability and quality gates, not promised dates. A 0.x
  release is cut only after its behavior is dogfooded and explicitly approved.
- Open release-gate issues may outlive their implementation sprint. Never mark
  one complete merely to close a sprint; carry it as explicitly deferred work.
- v0.11.0 is the next published tag after the 2026-08-17 dogfood pass.
  Do not cut v0.10.1 from this main.
