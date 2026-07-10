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

- [x] #63 Auto-enter guided cleanup from default clean → PR #71 merged as `e67078b`
- [x] #64 Add --no-guide and preserve classic clean paths → PR #71 merged as `e67078b`
- [x] #65 Harden non-TTY guided fallback and explicit age routing → PR #71 merged as `e67078b`

### Batch 2 - v0.6.1 Docs And Dogfood

- [x] #66 Refresh docs and dogfood around default clean --dry-run → PR #73 merged as `edfa80f`

### Batch 3 - v0.6.1 Release

- [x] #67 Release v0.6.1 default guided clean → PR #74 merged as `788c69b`, tag `v0.6.1` published

### Batch 4 - v0.7.0 Checklist Follow-Up

- [x] #68 Design TTY checklist renderer for guided clean → PR #72 merged as `ad0400f`
- [x] #69 Implement TTY checklist UI with text fallback → shared selection model, TTY checklist mode, text fallback tests
- [x] #70 Release v0.7.0 guided checklist UI → PR #77 merged as `ca643c9`, tag `v0.7.0` published

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
- #66 relay-ready handoff is prepared as `req-20260709134455122` / `docs-dogfood-default-guided`; dispatch it after PR #71 lands so docs are based on the runtime route in `main`.
- #67 releases only after runtime, tests, docs, and dogfood verification are complete.
- #67 release handoff is prepared as `req-20260709141058411` / `release-v0-6-1-default-guided`; dispatch it after PR #71 and #66 land.
- Batch 1 should avoid broad TTY UI work. v0.6.1 may reuse the current textual guide; v0.7.0 owns the richer checklist polish.
- #68 design-only handoff is prepared as `req-20260709134834849` / `tty-checklist-design`; it must not implement the TTY renderer or add dependencies.
- #70 release handoff is prepared as `req-20260709141058572` / `release-v0-7-0-guided-checklist`; dispatch it after #68/#69 land and v0.6.1 status is resolved.
- #68-#70 are intentionally after v0.6.1: default routing should ship before introducing a richer terminal renderer.

## Progress

- 2026-07-09: Created active sprint for epic #62 and split work into v0.6.1 runtime/docs/release followed by v0.7.0 checklist follow-up.
- 2026-07-09: Dispatched Batch 1 (#63-#65) through relay-ready/relay-plan as `default-guided-runtime`, run `issue-63-20260709130417141-761d43bf`.
- 2026-07-09: Batch 1 reached relay `ready_to_merge` in PR #71. Internal and post-publication relay reviews passed, local `go test ./...`, `go build ./...`, `go vet ./...` passed with `GOCACHE=/private/tmp/aibris-gocache-63`, and GitHub ubuntu/macos CI passed.
- 2026-07-09: Captured #66 dogfood from PR #71 head: default `clean --dry-run --root ~/.codex` entered guided cleanup, selected 3 items / 3.1 GB, protected 36 items / 30.8 GB, and confirmed dry-run deletion safety. Persisted relay-ready handoff `req-20260709134455122` and commented evidence on #66.
- 2026-07-09: Prepared relay-plan artifacts for #66 (`/tmp/dispatch-66.md`, `/tmp/rubric-66.yaml`) and dry-run validated dispatch, still gated on PR #71 landing. Prepared #68 relay-ready and relay-plan artifacts (`req-20260709134834849`, `/tmp/dispatch-68.md`, `/tmp/rubric-68.yaml`) and dry-run validated design-only dispatch.
- 2026-07-09: Prepared release handoffs and relay-plan artifacts for #67 (`req-20260709141058411`, `/tmp/dispatch-67.md`, `/tmp/rubric-67.yaml`) and #70 (`req-20260709141058572`, `/tmp/dispatch-70.md`, `/tmp/rubric-70.yaml`), with dry-run dispatch validation passing. GitHub issue comments were posted through the connector after local `gh` could not connect to `api.github.com`.
- 2026-07-09: PR #71 received a Codex P1 review that `--force` must not auto-enter guided cleanup. Fixed on PR branch commit `e5490cc` by treating `--force` as a classic selector while preserving explicit `--guide --force`; verified `go test ./...`, `go build ./...`, `go vet ./...`, and backlog doctor locally. GitHub Actions CI passed, the Codex inline thread was replied to and resolved, and CodeRabbit is processing the new commit.
- 2026-07-09: PR #71 received follow-up review on protected-only guided rows and a transient remote blob typo. Fixed routing to require selected guided targets before default reroute, restored the interactive skip print, and advanced PR #71 to `cdd08d0`. Local focused tests passed, GitHub Actions run `29030455743` passed on ubuntu and macOS, and all PR review threads are resolved. Next batch remains gated on PR #71 landing.
- 2026-07-10: Squash-merged PR #71 as `e67078b`, closing #63-#65 and opening the #66 docs/dogfood gate. Started #66 on `codex/issue-66-docs-dogfood` from `origin/main`.
- 2026-07-10: Squash-merged PR #73 as `edfa80f`, closing #66 and opening the v0.6.1 release gate. Started #67 on `codex/issue-67-release-v0.6.1` from `origin/main`.
- 2026-07-10: Squash-merged PR #74 as `788c69b`, pushed annotated tag `v0.6.1`, verified release workflow `29057968415`, confirmed GitHub Release assets, and smoke-tested `install.sh` returning `aibris version 0.6.1`.
- 2026-07-09: #68 design produced in `docs/GUIDED_CLEAN_TTY_CHECKLIST.md`; task AC checked locally and PR publication is pending verification.
- 2026-07-10: Squash-merged PR #72 as `ad0400f`, closing #68. Implemented #69 on `codex/issue-69-tty-checklist-ui`: shared recommended/reviewable/locked selection model, TTY checklist mode routing, projected freed-space totals, age threshold replanning, and text fallback regression tests.
- 2026-07-10: Squash-merged PR #76 as `dd85f64`, closing #69. Started #70 release prep on `codex/issue-70-release-v0.7.0` with v0.7.0 changelog and install example updates.
- 2026-07-10: Verified #70 release prep locally: `go test ./...`, `go build ./...`, `go vet ./...`, backlog doctor, `goreleaser release --snapshot --clean`, non-TTY dry-run dogfood, and pseudo-TTY dry-run dogfood all passed without deletion.
- 2026-07-10: Squash-merged PR #77 as `ca643c9`, pushed annotated tag `v0.7.0`, verified release workflow `29058905357`, confirmed GitHub Release assets plus `checksums.txt`, and smoke-tested `install.sh` returning `aibris version 0.7.0`.
