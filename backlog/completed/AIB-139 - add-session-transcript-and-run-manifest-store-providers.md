---
id: AIB-139
title: Ship the read-only protected-content retention inventory (codex-sessions)
status: Done
labels:
  - enhancement
  - cli
  - scanner
  - safety
  - type:feature
priority: high
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
updated_date: '2026-08-06'
---
## Description

## Goal

Surface the agent session, transcript, and run-manifest stores as a **read-only
retention inventory** rather than as safety decisions. This is the largest
uncovered area — `~/.codex/sessions` alone was 11 GB with 85% of its 6,711 files
older than 30 days — and it is user content: the obligation is to surface it,
not to reclaim it.

Also measured in the 2026-07-26 audit: `~/.relay/runs` 933 MB,
`~/.cursor/chats` 674 MB, `~/.claude/projects` 502 MB, `~/.gstack/projects`
91 MB. Those stores are future provider work under the same
root/unit/timestamp discipline; only `codex-sessions` ships in this leaf.

## Re-scope (2026-08-06)

Issue #139 was reduced from a full retention pipeline (provider + selector + exact
manifest + executor) to a **read-only inventory**. The execution layer is
explicitly parked: no retention selector, no `--retention-bucket` flag, no
member manifest, no mutation path. Quiescence was a verification-protocol
artifact (A-B scan stability), not code, and is removed — drift is harmless for
a point-in-time read-only inventory, so no quiet-home window is required.

## Frozen contract

The canonical contract lives in
[docs/PROTECTED_RETENTION.md](../../docs/PROTECTED_RETENTION.md) and is now
shipped (not merely planned): bounded store registry, UTC-month bucket identity,
read-only aggregate projection, orphan statistics as evidence only, and
absolute exclusions. The JSON surface is additive at the top level
(`retention` object), documented in
[docs/JSON_SCHEMA.md](../../docs/JSON_SCHEMA.md).

## Acceptance criteria

- [x] The `codex-sessions` provider inventories `~/.codex/sessions`
      (`rollout-*.jsonl` leaves) without parsing conversation bodies, bucketed
      by leaf UTC-month `Lstat.ModTime`.
- [x] One aggregate row per `(store_id, bucket_id)` with `unit_count`,
      `member_count`, `apparent_bytes`, `orphaned_count`, `orphaned_bytes`;
      `unknown` bucket for unusable timestamps; missing store root is a
      complete empty inventory, not a partial error.
- [x] Orphan statistics reuse the recorded-cwd classifier (AIB-138 L1) on the
      first metadata record only, gated by producer `codex_cli_rs` + supported
      version + absolute non-NUL cwd. They are evidence only: they never emit
      `EntryClassOrphaned`, never create cleanup candidates.
- [x] Retention is additive and non-authorizing: rows never enter `summary`,
      `total_count`/`total_size`, or cleanup eligibility; retention-local
      partial state does not set top-level `partial` or change exit status.
- [x] No member path, session identifier, or transcript content appears in the
      projection or provider diagnostics (path-free errors).
- [x] Installed content is not misclassified as a store; symlinked,
      non-regular, non-rollout, and non-conforming entries are silently skipped.
- [x] Last-scan cache identity includes the retention provider set
      (`retention_provider_identity`).
- [x] Human output renders a "retention (protected content, read-only)" section.
- [x] Tests cover bucket accounting, orphan aggregation, `unknown` bucket,
      missing-root empty-complete, cancellation, silent skips, permission
      partiality, JSON shape invariance, and "inventory never creates cleanup
      candidates".
- [x] Real-home verification: full scan of the developer `$HOME` produced 12
      UTC-month buckets (~11.8 GB) with orphan statistics and no debris or
      retention partial state.
- [x] Docs updated: PROTECTED_RETENTION.md (shipped read-only contract),
      JSON_SCHEMA.md (additive `retention` object), SPEC.md, CATEGORY.md,
      DOGFOOD.md (quiescence no longer required), README.md, ROADMAP.md.

## Out of scope (parked)

- Retention selector, exact-member manifest, planner, executor,
  `--retention-bucket` flag.
- Cursor/Gstack/Claude/relay-runs store providers (future leaves under the
  same discipline).
- Codex SQLite and Cursor `ai-tracking` (inventory-only absent separate
  quiescence + atomic database-family contract).
