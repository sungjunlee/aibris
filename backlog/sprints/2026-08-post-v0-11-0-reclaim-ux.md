---
milestone: 0.11.x Protected-Weight Reclamation
status: active
started: 2026-08-17
due: TBD
scope: ["cmd/**"]
---

# post-v0-11-0-reclaim-ux

## Goal

Shipped 0.11 reclaim surfaces tell the operator how to recover remaining bytes without changing hard locks (`plain-dir` never-delete, strip Git fail-closed, `--apfs-snapshots` and `--pressure` stay non-default).

## Plan

### Batch 1 — copy/hint on disjoint files

- [x] #253 Hint --apfs-snapshots after strip/clean when local snapshots still hold blocks (~1hr)
- [x] #254 Point scan next at a reclaim ladder, not only clean --dry-run (~1hr)
- [x] #256 Rewrite --apfs-snapshots copy: local snapshots are not Time Machine backups (~15min)

### Batch 2 — summaries that share Batch 1 files

- [x] #255 Summarize strip planned vs freed vs kept reasons (~1hr)
- [x] #258 Say review-only worktrees are not a cleanup command (~1hr)

### Batch 3 — scan reuse after the copy/ladder lands

- [ ] #257 Reuse last scan between matching dry-run and execute, or say why not (~1hr)

## Running Context

- Hard locks stay closed: `plain-dir` is never a delete/strip candidate; strip keeps the unit when Git evidence is unavailable; `--apfs-snapshots` stays opt-in and is never auto-run from strip or classic clean; `--pressure` stays explicit except the existing critical-volume auto-relax inside `clean`.
- Batch 1 files are disjoint (`cmd/clean_strip.go`+`cmd/clean.go`, `cmd/scan.go`, `cmd/apfs_snapshots.go`) so the three PRs can land independently.
- Batch 2 landed after Batch 1: #255 closer accounts planned vs kept (Git keep stays a skip; planned count uses the original target list, not a cancelled subset). #258 adds a review-only next line that is not a clean command; paths stay hidden; `plain-dir` stays never-delete/never-strip.
- OSS track (#118, #120, #121, #122) stays out of this sprint; this track is `cmd/**` only.
- Do not implement #218 or #142 L2/L3.
- #253 is a human stdout hint after bytes are actually freed. It does not run `tmutil thinlocalsnapshots`, and it does not attach to `--json` (versioned receipt stays intact).
- #254 strip `next` estimate uses `selectStripTargets`, including the CWD refusal `clean --strip` already applies.

## Progress

- 2026-08-17: Admitted this sprint from live #253–#258 after v0.11.0 dogfood. Goal is reclaim UX, not wider deletes. Next: Batch 1 as three small PRs.
- 2026-08-17: #256 in review — human `--apfs-snapshots` copy drops urgency tokens and states local snapshots are not Time Machine backups.
- 2026-08-17: Batch 1 merged. #256 via PR #259, #253 via PR #260, #254 via PR #261. Independent Sol review requested JSON-route hint (#253) — declined to keep the versioned JSON receipt intact. Next: Batch 2 (#255, #258).
- 2026-08-17: Batch 2 merged. #255 via PR #263, #258 via PR #264. Sol asked #255 to keep planned count as `len(targets)` on cancel — accepted. Next: Batch 3 (#257).
