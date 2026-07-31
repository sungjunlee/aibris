---
id: AIB-139
title: Add session, transcript, and run-manifest store providers with retention buckets
status: In Progress
labels:
  - enhancement
  - cli
  - scanner
  - safety
  - type:feature
priority: high
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
---
## Description
## Goal

Discover the agent session, transcript, and run-manifest stores and present them
as retention decisions rather than safety decisions. This is the largest
uncovered area — `~/.codex/sessions` alone was 11 GB with 85% of its 6,711 files
older than 30 days — and it is user content.

Also uncovered: `~/.relay/runs` 933 MB, `~/.cursor/chats` 674 MB,
`~/.claude/projects` 502 MB, `~/.gstack/projects` 91 MB.

## Frozen L1 contract

L1 is complete as a Markdown-only contract:
[Protected-Content Retention Contract](../../docs/PROTECTED_RETENTION.md).
It freezes the bounded store registry, UTC-month projection, explicit selector,
exact-member manifest, non-recursive execution, mutation-time revalidation,
and #138/#151 precedence. The named providers, JSON projection,
`--retention-bucket` flag, planner, and executor remain unshipped and belong to
later leaves.

No retention-only row authorizes default cleanup. Aggregate rows are
non-additive inventory projections and never executable `DebrisInfo` rows.
Installed content remains absent from every provider, inventory, manifest,
selector, and cleanup surface. Codex SQLite and Cursor AI tracking remain
inventory-only in planning until their separate producer-cooperation and
atomic-family obligations are met.

## Acceptance criteria

- [ ] Providers discover codex sessions, cursor chats, claude projects, relay
      runs, and gstack projects.
- [ ] `~/.codex/sessions` orphaned-session detection lands here (moved from
      AIB-138). Reuse the recorded-cwd classifier from AIB-138 L1 and report an
      aggregate orphaned count and size per time bucket, not one item per file.
- [ ] Size is reported per store and per time bucket, not as one opaque total.
- [ ] Transcripts are never cleaned by default and not by `--risky` alone; an
      explicit retention selector is required.
- [ ] Age comes from the transcript's own timestamp. Document why age is
      meaningful here and structurally broken for global caches, whose mtime
      tracks continuous use and can never satisfy `--age 7d`.
- [ ] Installed content is not misclassified as a store.
- [ ] Full-home scan performance follows the same-session paired-delta protocol
      in `docs/DOGFOOD.md`; report change-minus-base deltas with cache condition
      and observed scale instead of comparing an absolute stored timing.
- [ ] Tests cover bucket accounting and the default-protected contract.
