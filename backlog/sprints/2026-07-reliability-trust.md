---
milestone: 0.8.x Reliability & Trust
status: active
started: 2026-07-23
due: TBD
objectives: []
component: ""
---

# 0.8.x Reliability & Trust

## Goal

The default cleanup path exposes every relevant candidate, CLI failures are
unambiguous to humans and automation, and the next 0.8.x patch is ready for an
explicit maintainer release decision.

## Plan

### Batch 1 — Make CLI outcomes truthful

- [x] #106 Validate clean category and tool selector values (~45min) → PR #131 (merged)
- [x] #107 Propagate cleanup failures through process exit status (~60min) → PR #131 (merged)

### Batch 2 — Make inventory completeness explicit

- [x] #108 Define complete versus partial scan semantics (~60min) → PR #131 (merged)

### Batch 3 — Repair the default cleanup journey

- [~] #105 Keep all-category cleanup visible when guided review activates (~2h) → PR #132 (reviewing)

### Batch 4 — Lock the public CLI contract

- [ ] #109 Add black-box CLI contract tests (~90min)

### Batch 5 — Refresh public trust surfaces

- [ ] #110 Refresh public security, community, and roadmap contracts (~45min)

### Batch 6 — Close the milestone without forcing a version

- [ ] #104 [Epic] Harden CLI contracts and public trust (~20min)
- [ ] #111 Prepare the next 0.8.x reliability patch release (~60min)

## Running Context

- GitHub Issues are the task source of truth; `backlog/tasks/` contains thin
  mirrors and this file owns execution order and cross-task decisions.
- The project stays in 0.x until the maintainer is satisfied. No v1.0.0 target
  or implied deadline belongs in this sprint.
- Real-home DX evidence on 2026-07-22 found 21.3 GB in 14.47s. Plain guided
  dry-run selected 0 B while `--no-guide --dry-run` exposed 2.8 GB eligible;
  #105 must close that gap without weakening worktree safety.
- Hard safety remains non-negotiable: `--force` may skip only final
  confirmation and must never unlock protected rows or force Git removal.
- Batch 1 establishes selector and exit contracts before Batch 4 locks them
  with compiled-process tests. Batch 2 settles completeness semantics before
  the later automation schema milestone builds on them.
- Batch 3 must preserve every hard lock and explicit selector while ensuring
  guided Codex review cannot hide caches, dependencies, or orphaned worktrees.
- Batch 4 uses an isolated temporary HOME. Batch 6 is a verification and
  maintainer-approval gate, not a fixed release date.

## Progress

- 2026-07-23: Created no-date 0.x roadmap milestones #6-#9, opened issues
  #104-#129, mirrored all open issues locally, and activated this sprint for
  milestone #6. No v1.0.0 milestone was created.
- 2026-07-23: Implemented and verified #106/#107 on
  `codex/reliability-trust`. Invalid selectors now fail before scanning, and
  classic/interactive/guided execution failures preserve receipts and return
  non-zero. Full tests, build, and vet pass; issues remain in progress until
  the branch is published and reviewed.
- 2026-07-23: Published PR #130 and moved #106/#107 to `status:in-review`.
- 2026-07-23: PR #130 CI passed on macOS and Ubuntu; CodeRabbit reported no
  blocking review result because its review quota was rate-limited.
- 2026-07-23: Squash-merged PR #130. Reopened #106/#107 after finding two
  actionable inline review comments through the API: preserve `--tool unknown`
  compatibility and record interactive safety rejections in cleanup receipts.
  Started both follow-ups with #108 on `codex/partial-scan-contract`.
- 2026-07-23: Published PR #131 for the #106/#107 review follow-ups and #108.
  Partial scans now retain usable results but label failed providers, emit
  machine-readable failure metadata, exit non-zero, invalidate the cleanup
  cache, and cannot authorize clean. Full tests, build, vet, and diff checks
  pass locally.
- 2026-07-23: PR #131 passed macOS and Ubuntu CI and was squash-merged. Closed
  #106/#107/#108 and removed their transient `status:in-review` labels.
- 2026-07-23: Started #105 on `codex/all-category-guided-clean`. The scoped
  design keeps guided Codex worktree review intact, then continues into the
  classic all-category audit while normalizing dry-run overlaps.
- 2026-07-23: Implemented #105. Guided review now continues to classic
  candidates, selected guided parents win symmetric path-overlap
  normalization, locked active worktrees remain outside classic selection,
  and non-TTY EOF behavior is documented. Full tests, build, vet, and diff
  checks pass locally.
- 2026-07-23: Published PR #132 and moved #105 to `status:in-review`.
  
