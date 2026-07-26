---
milestone: 0.10.x Agent State Store Coverage
status: active
started: 2026-07-26
due: TBD
objectives: []
component: ""
---

# 0.10.x Agent State Store Coverage

## Goal

Raise discovery of the agent-produced debris surface from about 15% to above
90%, without weakening any hard safety lock, while treating transcripts as user
content that is surfaced rather than reclaimed.

## Plan

### Batch 1 — Establish the proof-based tier (gating)

- [ ] #138 Add provable-orphan cleanup for agent session stores (~3h)

### Batch 2 — Fix discovery shape and cover byproducts

- [ ] #140 Replace bounded worktree discovery with a container registry (~3h)
- [ ] #142 Add agent byproduct store providers (~2h)

### Batch 3 — Cover the largest stores

- [ ] #139 Add session, transcript, and run-manifest store providers with retention buckets (~4h)

### Batch 4 — Open guided review to every tool

- [ ] #141 Generalize guided worktree review beyond codex (~3h)

### Batch 5 — State the boundary

- [ ] #143 Reframe the documented product boundary around agent debris (~2h)

### Batch 6 — Close capability scope

- [ ] #137 [Epic] Cover the agent state store surface (~30min)

## Running Context

- Evidence baseline is the 2026-07-26 coverage audit in `docs/DOGFOOD.md`. Every
  child issue carries its own measured numbers; re-measure rather than trusting
  restated totals.
- #138 is the gate. It is the first category with no age gate and a proof-based
  safety argument, so it is the real test of whether the #113 plan model absorbs
  new categories. Land it before #115 finalizes the execution contract.
- Batch 2 items are independent of #138 and of each other, so they may fan out.
  Batch 3 depends on #138's recorded-cwd readers.
- Orphan detection must read each store's recorded working directory. Directory
  names are a lossy encoding — `/`, `.`, and `_` all collapse to `-` — and
  decoding them produced false positives during the audit.
- Metadata only. Recorded working directory, timestamps, and file metadata are in
  scope; conversation bodies are never parsed for content.
- Transcripts are user content. Surfacing them with time buckets is in scope;
  reclaiming them by default is not, and `--risky` alone must not be enough.
- Installed content — `~/.claude/skills`, `~/.codex/plugins`,
  `~/.cursor/extensions` — is never debris. aibris already gets this right and
  every new provider must preserve it.
- Every existing hard lock, the non-forced `git worktree remove` contract, and
  the preflight/verification behavior stay unchanged.
- Report full-home scan time against the 19.2s baseline whenever a provider is
  added.
- The project remains in 0.x. This milestone has no due date and implies no
  v1.0.0 schedule.

## Progress

- 2026-07-26: Opened the milestone from the coverage audit. Paused the 0.9.x
  sprint so exactly one sprint owns execution; #115, #116, #112, and the #117
  release gate remain open and unchanged in milestone #7.
