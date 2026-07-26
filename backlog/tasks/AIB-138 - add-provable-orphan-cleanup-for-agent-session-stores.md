---
id: AIB-138
title: Add provable-orphan cleanup for agent session stores
status: To Do
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
- [ ] Readers exist for `~/.claude/projects/<key>/*.jsonl`,
      `~/.cursor/projects/<key>/worker.log`, and
      `~/.codex/sessions/**/rollout-*.jsonl`.
- [ ] An entry whose cwd cannot be determined is undetermined and never cleanable.
- [ ] Metadata only; conversation bodies are never parsed for content.
- [ ] Orphans are eligible under default `clean` without `--risky` and without `--age`.
- [ ] `scan --json` exposes the classification and the absent cwd as the reason.
- [ ] The category populates the AIB-113 plan model without duplicating policy,
      with correct overlap accounting.
- [ ] Tests cover live, orphaned, and undetermined entries plus lossy-name fixtures.
