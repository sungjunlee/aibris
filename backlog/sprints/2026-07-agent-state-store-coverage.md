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

- [x] #138 Add provable-orphan cleanup for agent session stores; L1–L3 merged through PRs #144, #148, and #154

### Prerequisite hardening — complete

- [x] #150 Make the agent-state revalidation gate fail closed → PR #153 (merged `fdfb9b1`, 4 review rounds)

### Batch 2 — Freeze cache and measurement contracts

- [x] #145 Invalidate the cleanup scan cache with a stable provider-membership identity; behavior revisions still bump the cache revision → PR #155 (merged `c49b4e4`)
- [ ] #146 Replace the absolute scan-time baseline with same-session paired deltas; keep the repeatable harness in #129 (~1h)

### Batch 3 — Close nested agent-state safety

- [ ] #151 L1 overlap safety: protected agent-state shields its subtree and outer targets inherit nested safety/revalidation (~2h)
- [ ] #151 L2 plan/audit accounting: outermost physical target owns bytes while rows, reasons, and receipts retain nested obligations (~1h)

### Batch 4 — Fix bounded worktree-container coverage

- [ ] #140 Cover the confirmed `~/.config/superpowers/worktrees` depth gap within a documented finite discovery bound (~3h)

### Batch 5 — Classify agent byproducts

- [ ] #142 L1 store classification: freeze installed, regenerable, and user-content decisions before adding providers

### Batch 6 — Introduce protected-content retention semantics

- [ ] #139 L1 retention contract: freeze bucket, selector, aggregation, timestamp, and execution-manifest semantics

### Batch 7 — Add byproduct coverage

- [ ] #142 `L2-regenerable-providers` — add only regenerable residue providers.
- [ ] #142 L3 protected artifacts: surface user artifacts without implicit cleanup through default clean or `--risky`

### Batch 8 — Cover the largest retention stores

- [ ] #139 L2 Codex sessions: aggregate counts and sizes by time bucket without parsing conversation bodies
- [ ] #139 L3 Cursor/Gstack/Claude: add coverage without duplicate rows or bytes
- [ ] #139 L4 relay runs and end-to-end retention CLI contract

### Batch 9 — Open guided review to every tool

- [ ] #141 Generalize guided worktree review beyond codex (~3h)

### Batch 10 — State the boundary

- [ ] #143 Reframe the documented product boundary around agent debris (~2h)

### Batch 11 — Close capability scope

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
- #145 and #146 are code-path independent, but their sprint/task/issue updates
  remain orchestrator-owned. Run later provider leaves sequentially because
  their provider registry, public docs, and real-home measurement surfaces
  overlap.
- #151 is a safety gate for #142 and #139. A protected `live` or `undetermined`
  agent-state entry shields its complete subtree. When a generic outer target
  contains an orphaned agent-state entry, the outer target owns physical bytes
  but must inherit the child's deletion-time revalidation obligation.
- #142 and #139 interleave deliberately: classify byproducts first, freeze the
  protected-content retention contract, then add regenerable and protected
  providers. `generated_images` and transcripts require an explicit retention
  decision; `--risky` alone is insufficient.
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
- Report provider performance as a same-session paired delta: build base and
  change together, alternate runs, and record cache condition and observed
  scale. Keep 19.2s only as a labelled historical observation.
- #147 returns to the paused 0.9.x execution-contract stream after #151; it
  should reuse #115's single plan pipeline rather than add another normalization
  implementation in this milestone.
- #152 remains an explicit maintainer-approved limitation until an actual
  Windows host or Windows CI runner can verify `GetVolumePathNameW`. Do not ship
  unverifiable safety-barrier syscall code merely to close the milestone.
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
- **Key the cache by concrete provider membership.** Otherwise a snapshot from
  a previous binary is silently accepted and the new category vanishes from
  `clean`. Verified A/B: `matched 0 candidates` before invalidation, `matched
  81` after. #145 derives a sorted membership identity from the registry;
  behavior changes inside unchanged providers still bump the explicit cache
  revision. Merged-main real-home verification wrote a legacy cache with
  `e71be9d`, observed the new binary reject it via `scan live`, then observed
  the same new cache reuse via `scan cached, 6s old`; both new-binary paths
  agreed on 276 agent-state rows, 195 eligible, and 81 protected.
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
- 2026-07-28: Replanned the remaining milestone after Batch 1. `codexbar`
  reported Codex weekly usage at 7% and code-review usage at 6%, so Codex is the
  primary executor and reviewer route; Claude remains a limited independent
  review fallback. An independent Codex/Sol-high planning audit confirmed that
  only #145/#146 are plausibly parallel and found that #151 also owns nested
  deletion-time revalidation, not merely byte accounting.

  Added #145, #146, and #151 as explicit gates. Narrowed #140 to the measured
  superpowers depth gap, split #142 into classification/regenerable/protected
  leaves, and split #139 into retention-contract plus three provider leaves.
  Routed #147 back behind #115 and explicitly deferred #152 until a real Windows
  verification environment exists.
- 2026-07-28: Started #145 as the frozen `provider-cache-identity` leaf on
  `issue-145-provider-cache-identity`. The contract separates concrete provider
  membership from the explicit behavior revision, rejects legacy or mismatched
  membership to the visible live-scan path, and preserves matching-cache reuse.
  Because incompatible reuse can silently omit cleanup candidates, the relay run
  uses hardened pre-publication and post-publication review.
- 2026-07-29 13:25: #145 dispatched → PR #155 → hardened review (LGTM,
  final replacement run round 2) → squash-merged as `c49b4e4`; #145 closed and
  the run worktree plus local/remote branch were cleaned. Merged `main` passed
  race tests, build, vet, and Linux/Windows/Darwin builds.

  Real-home same-session verification used `e71be9d` to write a legacy
  identity-free cache, then the merged binary's `clean --no-guide --dry-run`
  correctly reported `scan live`; a second merged-binary dry-run reported
  `scan cached, 6s old`. The full live plan remained **198 items / 1.6 GB**.
  Both paths agreed on agent-state **276 found / 195 eligible (228.7 MB) / 81
  protected**. No files were removed.

  Review exposed four follow-ups: aibris #156 binds cache identity to the
  producing scanner and closes the concrete-membership end-to-end test gap;
  aibris #157 makes last-scan writes atomic; dev-relay #1117 prevents a rejected
  round-cap retry from consuming reviewer-swap quota; dev-relay #1118 prevents
  executor process success from attesting failed verification and permits
  operator evidence on clean no-op runs.
