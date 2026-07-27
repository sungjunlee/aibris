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

- [~] #138 Add provable-orphan cleanup for agent session stores (~3h)
  - relay-ready request `req-20260726233000943`, three ordered leaves
  - [x] `L1-claude-store-classification` → PR #144 (merged, 15 review rounds).
        `agent-state` category, additive `Classification` field,
        `~/.claude/projects` recorded-cwd reader, **and** the eligibility rule,
        which moved here from L3 after the original contract proved
        self-contradictory.
  - [ ] `L2-cursor-store-classification` — same classifier via `worker.log`,
        migrate `~/.cursor/projects` off `ai-logs`
  - [ ] `L3-orphan-clean-eligibility` — narrowed: `UnifiedCleanupPlan` absorption,
        audit/dry-run/receipt surfaces, compiled CLI contract test, docs.
        Eligibility already landed in L1.

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

### Patterns established by L1 — apply to every remaining leaf and provider

L1 took 15 review rounds, and most findings were the same few shapes. Encoding
them in the next Done Criteria up front should cut that sharply.

- **One eligibility owner.** `internal/cleaner/EvaluateEligibility` is consulted
  by `cleaner.Filter`, `cmd/clean_audit.go`, and `summarizeCleanup` in
  `cmd/scan.go`. Adding a provider must require no change at any consumer, and no
  fourth consumer may reimplement the rule. Three separate rounds were spent
  discovering these consumers one at a time.
- **cwd evidence discipline.** Never decode a store's directory name — the
  encoding is lossy. Read the recorded cwd. Require unanimity across every
  recorded cwd, let any live cwd win, and treat unreadable or unparseable
  evidence as `undetermined` rather than `orphaned`. A complete record or file
  that simply carries no cwd is **not** evidence and must not force
  `undetermined`; treating it as ambiguity would have reduced orphan detection to
  zero.
- **Path availability over the whole chain.** Use `Lstat` on the recorded cwd
  *and* every ancestor. A present-but-unresolvable symlink, or a nearest existing
  ancestor that is a mount root, means the surrounding tree is unavailable, not
  that the path was deleted. This surfaced three times at three path positions.
- **Revalidate before deleting.** The scan snapshot can be five minutes old.
  Re-derive the classification immediately before removal and fail closed, the
  same discipline the Git-aware worktree executor already uses.
- **Bump the cache schema when the provider set changes.** Otherwise a snapshot
  from a previous binary is silently accepted and the new category vanishes from
  `clean`. Verified A/B: `matched 0 candidates` before the bump, `matched 81` after.
  #145 replaces the manual constant with a derived key.
- **Carry a real-home invariant as a Done Criterion.** L1 held
  `81 orphaned / 44 live / 11 undetermined` through every safety tightening. That
  is what stops a fail-closed change from quietly zeroing detection.
- **Measure as a same-session delta.** The absolute `19.2s` scan baseline is not
  reproducible — 11s to 39s on one machine depending on cache state. See #146.
- **Put obligations where the capability is.** A Done Criterion asking the
  executor to write the PR body cannot be met; the publish step generates it.

## Progress

- 2026-07-26: Opened the milestone from the coverage audit. Paused the 0.9.x
  sprint so exactly one sprint owns execution; #115, #116, #112, and the #117
  release gate remain open and unchanged in milestone #7.
- 2026-07-26: Shaped #138 through proposal-first relay-ready after route
  preflight returned `needs_split`. Persisted `req-20260726233000943` with three
  ordered leaves. Two decisions: an additive `Classification` field rather than
  widening `DebrisInfo.Status`, which keeps the JSON `status` domain frozen for
  #124; and migrating `~/.cursor/projects` from `ai-logs` to a non-risky
  `agent-state` category with orphan-only default eligibility. Shaping also moved
  the `~/.codex/sessions` reader out of #138 into #139 — a day directory holds
  sessions from many working directories, so there is no directory-level cwd to
  classify against and per-file reporting would emit 6,711 items. Issue bodies,
  task files, and both issues' comments record the move.
- 2026-07-27: L1 dispatched → PR #144 → reviewed (LGTM, round 15) → merged as
  `d9054b8`. First real capability from this milestone: 81 orphaned Claude
  project-store entries, 161,634,387 B, in an area that previously reclaimed
  0 bytes. The count matches the pre-implementation Python audit exactly.
  Three follow-up issues opened from findings: #145 cache provider-set keying,
  #146 scan-timing methodology, #147 scan/clean normalization parity.
