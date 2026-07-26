---
id: AIB-139
title: Add session, transcript, and run-manifest store providers with retention buckets
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

Discover the agent session, transcript, and run-manifest stores and present them
as retention decisions rather than safety decisions. This is the largest
uncovered area — `~/.codex/sessions` alone was 11 GB with 85% of its 6,711 files
older than 30 days — and it is user content.

Also uncovered: `~/.relay/runs` 933 MB, `~/.cursor/chats` 674 MB,
`~/.claude/projects` 502 MB, `~/.gstack/projects` 91 MB.

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
- [ ] Full-home scan time is reported against the 19.2s baseline.
- [ ] Tests cover bucket accounting and the default-protected contract.
