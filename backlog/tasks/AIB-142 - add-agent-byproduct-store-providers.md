---
id: AIB-142
title: Add agent byproduct store providers
status: To Do
labels:
  - enhancement
  - cli
  - scanner
  - type:feature
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
---
## Description
## Goal

Cover the byproduct stores agents leave behind, and settle one unclassified
store. Measured uncovered: `~/.codex/packages` 1.0 GB (classification unknown),
`generated_images` 548 MB, `sqlite` 412 MB, `tmp` 130 MB, `computer-use` 61 MB,
`~/.cursor/ai-tracking` 35 MB.

## Acceptance criteria

- [ ] Providers cover codex `generated_images`, `sqlite`, `tmp`, `computer-use`,
      and cursor `ai-tracking`.
- [ ] `~/.codex/packages` is investigated and explicitly classified as installed
      content or residue; the finding is recorded in `docs/CATEGORY.md`.
- [ ] Each store is documented as regenerable or not, and default-clean
      eligibility follows from that classification rather than from size.
- [ ] Agent-produced user artifacts such as `generated_images` are not default-clean.
- [ ] Installed content stays excluded.
