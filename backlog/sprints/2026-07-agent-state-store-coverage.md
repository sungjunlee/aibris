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

### Batch 1 — Establish the proof-based tier (gating) — complete

- [x] #138 Add provable-orphan cleanup for agent session stores (~3h)
  - relay-ready request `req-20260726233000943`, three ordered leaves
  - [x] `L1-claude-store-classification` → PR #144 (merged, 15 review rounds).
        `agent-state` category, additive `Classification` field,
        `~/.claude/projects` recorded-cwd reader, **and** the eligibility rule,
        which moved here from L3 after the original contract proved
        self-contradictory.
  - [x] `L2-cursor-store-classification` → PR #148 (merged `8b86fe4`,
        17 review rounds). Shared classifier extracted to `recorded_cwd.go`,
        `~/.cursor/projects` migrated to `agent-state`, recorded cwd read from
        `workspacePath=`. Six defects found after the code first passed clean —
        five of them would have deleted a live workspace's store.
  - [x] `L3-orphan-clean-eligibility` → PR #154 (merged `b30df08`,
        5 review rounds). Proved `UnifiedCleanupPlan` absorption, added the
        `agent_state_orphaned` reason code, locked audit/dry-run/receipt
        surfaces with a compiled CLI contract test, and reconciled docs.
        Eligibility already landed in L1.

### Batch 2 — Fix discovery shape and cover byproducts

- [x] #150 Make the agent-state revalidation gate fail closed (~1h) → PR #153
      (merged `fdfb9b1`, 4 review rounds). Prerequisite for #139 and #142, both
      of which add `agent-state` providers.
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
- #138 was the gate. It is the first category with no age gate and a proof-based
  safety argument. L3 proved the #113 plan model absorbs it without structural
  change: `ClassicCleanupPlanCandidates` consumes `cleaner.Filter` output and
  does not distinguish proof-based from age-based eligibility. The only plan
  model change was the 12-line `agent_state_orphaned` reason code. This is the
  signal #115 was waiting on.
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
  executor to write the PR body cannot be met. Note the publish step does not
  generate one either — `publish-run.js` emits a stub (`Relay run <id>`), so the
  measurement write-up is the orchestrator's job.

### Patterns established by L2 — the measurement blind spot

L2 took 17 rounds. Unlike L1, the code passed contract and quality on round 1
with zero findings; every defect after that was found by *reading* rather than by
measuring, because the real home lacked the triggering condition.

- **Real-home measurement cannot find what the home does not contain.** L2's
  orphan count was verified twice — once by the orchestrator before dispatch,
  once by the executor with an independent script — and both agreed exactly at
  `109 / 74,689,446 B`. Both were wrong in the same way. No workspace path on
  this machine contains a space, no entry records multiple cwds, no orphan sits
  at a mount root. Agreement between two measurements sharing a blind spot is not
  verification.
- **Write adversarial parser fixtures before measuring.** Every parser reading
  real-world files needs fixtures for interior whitespace, truncated final
  records, multiple records, and values that resolve to a binary rather than a
  workspace — *before* trusting any count. Three of L2's five deletion bugs would
  have been caught pre-dispatch by that alone.
- **A migration must reconcile every contract, not the obvious three.** L2's
  criteria named CHANGELOG, `docs/CATEGORY.md`, and `docs/JSON_SCHEMA.md`. The
  routing claim actually lived in eight places, including `AGENTS.md`,
  `docs/SPEC.md`, `SECURITY_AUDIT.md`, `README.md`, and `cmd/root.go` help text.
  Rounds 5–7 went to finding them one at a time. Write "grep the repository and
  reconcile every claim" into the criterion.
- **Platform-specific code needs a cross-compile in the criteria.** A
  `syscall.Stat_t` reference broke the Windows release target; CI ran only ubuntu
  and macOS, so it would have surfaced at release. `GOOS=windows go build ./...`
  is now a CI job.
- **Fail-closed can fail in the useless direction.** On Windows the device lookup
  returns a no-op rather than an error, because an error propagates as "absence
  not proven" and would make every entry `undetermined`, zeroing reclamation on
  that platform. Fail-closed is right only when the closed state is still useful.
- **Verify `repeat` findings, trust `deepening`.** Held again in L2:
  `deepening` findings were genuine every time, including two the orchestrator
  had already reviewed and waved through.

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
- 2026-07-27: L2 dispatched → PR #148 → 17 review rounds → merged as `8b86fe4`.
  `~/.cursor/projects` now reports `agent-state`: **111 orphaned /
  75,648,816 B** reclaimable by default where the category previously returned
  0 bytes, with 16 live and 11 undetermined protected. Combined with L1, default
  `clean` now plans 193 agent-state items / 227.2 MB. The `~/.claude` invariant
  held byte-identical throughout at 82 orphaned / 162,601,007 B / 11
  undetermined.

  The audit's `42 / 31.5 MB` cursor baseline was found unreproducible before
  dispatch and abandoned as a target: 101 of the orphans are former
  `~/.relay/worktrees` paths, which relay creates and reclaims every run, so the
  count moves continuously — it drifted 109 → 111 during this run alone. Done
  Criteria now require agreement with a same-session independent measurement.
  Same failure mode as the `19.2s` scan baseline (#146).

  The specified cwd rule ("first absolute path not under `~/.cursor`") was
  measured wrong before dispatch — it resolves to the npx binary and finds
  0 orphans across all 134 entries. Corrected to `workspacePath=` on #138.

  Five further defects surfaced only in review, each of which would have deleted
  a **live** workspace's store by default with no age gate: whitespace truncation
  in the recorded path, reading only the first of several recorded paths, an
  unterminated final log line accepted as complete, the mount-root barrier gap,
  and — separately — a broken Windows release build. All fixed and fixture-
  verified.

  Four follow-ups opened: #149 mount-root barrier (fixed in this PR, closed),
  #150 revalidation gate is a tool allowlist, #151 targets nested inside
  protected entries, #152 Windows volume-boundary detection. **#150 should land
  before #139 and #142**, since both add `agent-state` providers and a missing
  allowlist entry means deletion without revalidation rather than a compile
  error.
- 2026-07-28: #150 dispatched → PR #153 → 4 review rounds → merged as `fdfb9b1`.
  The agent-state revalidation gate now fails closed: revalidation is selected by
  a lookup keyed on `types.Tool`, and an item whose tool has no registered
  revalidator is refused rather than deleted. `adapter.AgentStateRevalidator` is
  an optional interface on the provider, and the provider list moved to
  `internal/adapter/providers.go` so scanning and revalidator lookup are built
  from one slice and cannot diverge. Behavior is unchanged — both binaries plan
  197 items / 1.6 GB, agent-state 194 eligible / 227.7 MB.

  **The L2 patterns paid for themselves: 17 rounds → 4.** Preloading them into
  the Done Criteria is what did it — the file-survival assertion, the fail-closed
  *direction*, cross-compile in verification, reference numbers labelled "not a
  target", and a round cap set once at the start instead of raised four times.

  The one substantive review finding was the L2 doc shape recurring exactly:
  the code was clean and single-source, but `AGENTS.md:39` and
  `CONTRIBUTING.md:41` still told contributors to register providers in
  `internal/scanner/scanner.go`. The reviewer's stated mechanism (two lists
  diverging) was wrong on source — there is only one list — but the risk was
  real, because an agent following those docs would find no list and could add a
  second one. Fixed in both files, each now also stating the revalidator
  obligation and why it exists. **When a change moves a declaration, grep for
  every document that names its old home** — this is now the second leaf in a row
  where that was the only real finding.
- 2026-07-28: #138 L3 dispatched → PR #154 → 5 review rounds → merged as
  `b30df08`; #138 closed and Batch 1 completed. The final blocking finding was a
  false positive against a stale 22-line PR-body snapshot; the live 119-line
  body contained all four requested items, so the maintainer authorized
  force-finalization on the evidence rather than a manufactured commit.

  Verification from merged `main`: `go test -race -count=1 ./...`,
  `go build ./...`, `go vet ./...`, and Linux/Windows/Darwin cross-builds all
  passed. The merged binary's real-home `clean --dry-run` remained unchanged at
  **198 planned items / 1.6 GB**; `agent-state` remained **276 found / 195
  eligible (228.6 MB) / 81 protected**. The plan model absorbed the
  age-independent category without structural change, resolving #115's open
  question; only the 12-line `agent_state_orphaned` reason code was needed.
