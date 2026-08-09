---
id: AIB-138
title: Add provable-orphan cleanup for agent session stores
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

**L1 merged 2026-07-27 as PR #144 (`d9054b8`), 15 review rounds.** Delivered the
`agent-state` category, the additive `Classification` field, the
`~/.claude/projects` recorded-cwd reader, and the classification-driven
eligibility rule. Measured: 81 orphaned entries / 161,634,387 B, matching the
independent pre-implementation audit exactly. L2 (cursor store) and L3 (plan-model
absorption, audit surfaces, docs) remain.

## Goal

Classify agent session-store entries whose recorded working directory no longer
exists, and make that class cleanable without an age gate. An absent recorded
cwd is a proof rather than a heuristic, and resume is already impossible for such
entries, so nothing is lost by removing them.

Measured on 2026-07-26: `~/.claude/projects` 81 orphans / 162 MB,
`~/.cursor/projects` 42 orphans / 31.5 MB.

## Acceptance criteria

- [ ] Orphan status is derived from the store's recorded cwd, never from decoding
      the directory name. The encoding is lossy — `/`, `.`, and `_` all collapse
      to `-` — and decoding produced false positives during the audit.
- [ ] Readers exist for `~/.claude/projects/<key>/*.jsonl` and
      `~/.cursor/projects/<key>/worker.log`. `~/.codex/sessions` moved to AIB-139
      during shaping: a day directory holds sessions from many working
      directories, so there is no directory-level cwd to classify against, and
      per-file reporting would emit 6,711 items.
- [ ] `DebrisInfo` gains an additive `Classification` field; the existing
      `Status WorktreeStatus` field and the JSON `status` value domain are
      unchanged, keeping AIB-124 schema work unblocked.
- [ ] `~/.cursor/projects` migrates from `ai-logs` to a new non-risky
      `agent-state` category. Only `orphaned` becomes default-clean; `live` and
      `undetermined` stay protected with reasons. The public contract change is
      stated in CHANGELOG and `docs/CATEGORY.md`.
- [ ] An entry whose cwd cannot be determined is undetermined and never cleanable.
- [ ] Metadata only; conversation bodies are never parsed for content.
- [ ] Orphans are eligible under default `clean` without `--risky` and without `--age`.
- [ ] `scan --json` exposes the classification and the absent cwd as the reason.
- [ ] The category populates the AIB-113 plan model without duplicating policy,
      with correct overlap accounting.
- [ ] Tests cover live, orphaned, and undetermined entries plus lossy-name fixtures.
