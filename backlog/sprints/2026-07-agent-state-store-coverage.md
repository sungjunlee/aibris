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

### Batch 2 — Freeze cache and measurement contracts — complete

- [x] #145 Invalidate the cleanup scan cache with a stable provider-membership identity; behavior revisions still bump the cache revision → PR #155 (merged `c49b4e4`)
- [x] #146 Replace the absolute scan-time baseline with same-session paired deltas; keep the repeatable harness in #129 → PR #158 (merged `755e233`)

### Batch 3 — Close nested agent-state safety — complete

- [x] #151 L1 overlap safety: protected agent-state shields its subtree and outer targets inherit nested safety/revalidation → PR #159 (merged `d348ede`, hardened review round 4)
- [x] #151 L2 plan/audit accounting: outermost physical target owns bytes while rows, reasons, and receipts retain nested obligations → PR #160 (merged `23f7b38`, hardened review round 3)

### Batch 4 — Fix bounded worktree-container coverage — complete

- [x] #140 Cover the confirmed `~/.config/superpowers/worktrees` depth gap within a documented finite discovery bound → PR #163 (merged `8caa554`, hardened primary review round 6)

### Batch 5 — Classify agent byproducts

- [x] #142 L1 store classification: freeze installed, regenerable, and user-content decisions before adding providers → PR #164 (merged `228e0f9`, hardened primary review round 5)

### Batch 6 — Introduce protected-content retention semantics — complete

- [x] #139 L1 retention contract: freeze bucket, selector, aggregation, timestamp, and execution-manifest semantics → PR #165 (merged `2d9714a`, 3 hardened primary rounds plus final exact-head review)

### Batch 7 — Add byproduct coverage — blocked on upstream producer cooperation

- [ ] #142 `L2-regenerable-providers` — add only regenerable residue providers.
- [ ] #142 L3 protected artifacts: surface user artifacts without implicit cleanup through default clean or `--risky`

### Batch 8 — Cover the largest retention stores

- [~] #139 L2 (re-scoped to **read-only inventory**) Codex sessions: aggregate
      counts, sizes, and orphan statistics by UTC month without parsing
      conversation bodies [branch:issue-139-readonly-retention-inventory] —
      implemented on a fresh branch from main; tests + docs green, PR pending
- [ ] #139 L3 Cursor/Gstack/Claude: add coverage without duplicate rows or bytes
      — parked with the execution layer; requires the parked selector/manifest
      machinery or a future re-scope leaf

### Batch 9 — Open guided review to every tool

- [~] #141 Generalize guided worktree review beyond codex (~3h) — implementation and full gates complete at `0a47323`; pre-publication parked on hardened advisory adapter availability [run:issue-141-20260731062303100-927250e4]

### Batch 10 — State the boundary

- [~] #143 Reframe the documented product boundary around agent debris (~2h)
      — README/SPEC/CATEGORY/ROADMAP reframed on
      [branch:issue-143-product-boundary-reframe]; PR pending

### Batch 11 — Close capability scope

- [x] #137 [Epic] Cover the agent state store surface — closed 2026-08-06 with
      the actionable scope shipped (#138/#139-re-scoped/#140/#142-L1/#143
      merged; #141 parked, #142 L2/L3 blocked upstream); re-audit deferred

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
- #142 L1's store-nature taxonomy is planning language, not a current
  `types.Category`, JSON classification, or CLI selector. `packages` and
  `computer-use` are installed content; `tmp` is only a future regenerable
  candidate; `generated_images` is a retention-selected user artifact; and
  Codex `sqlite` plus Cursor `ai-tracking` are inventory-only protected stores
  absent a separate quiescence and atomic database-family manifest contract.
  L2 and L3 are blocked until the relevant upstream producer documents a
  versioned layout/identity contract and exposes a cooperative exclusion
  protocol honored by every writer; aibris cannot manufacture that evidence.
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
- #139 was re-scoped on 2026-08-06: the shipped surface is a **read-only
  inventory** (codex-sessions UTC-month aggregates plus orphan statistics),
  with the retention selector, exact-member manifest, planner, and executor
  explicitly parked. Quiescence was a verification-protocol artifact (A-B scan
  stability), not code, and is removed: drift is harmless for a point-in-time
  read-only inventory, so the quiet-home window is no longer a gate. The
  inventory never authorizes cleanup; #138's proof-based orphan eligibility
  and #151 overlap hard locks are unchanged. L3/L4 (and #142 execution)
  remain parked and must not start from the unpublished old branch.
- #141 is likewise parked without weakening its hardened policy. Branch
  `issue-141-guided-review-all-worktree-tools` at `0a47323` is clean, pushed,
  current with `main`, and passes the exact-head race/build/vet gate plus
  Linux/Windows/Darwin builds. Codex primary review passed all 17 frozen Done
  Criteria and quality in rounds 2–4. The remaining Relay escalation is
  advisory infrastructure only: Pi lacks credentials, OpenCode returned useful
  findings with an invalid `line: 0` schema value, Antigravity returned no
  structured result, and ClinePass is not subscribed. OpenCode's recoverable
  findings were checked; the valid comparator/unknown-selector gaps were fixed
  and tested. Do not publish or merge until one healthy structured advisory
  round succeeds or the maintainer explicitly chooses a separate audited
  exception.
- Known worktree containers use a finite exact registry for `.codex`, `.relay`,
  `.gstack`, and `.config/superpowers/worktrees`; the generic depth-4 fallback
  remains bounded. Invalid metadata or mixed valid/invalid members protect the
  whole physical owner, and any active logical member protects a shared
  active/orphaned owner unless active cleanup is explicitly included.
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
- 2026-07-29 19:23: #146 dispatched → PR #158 → review (LGTM, round 5) →
  squash-merged as `755e233`; #146 closed, Batch 2 completed, and the run
  worktree plus local/remote branch were cleaned. Canonical #139 and #140
  acceptance criteria now require the paired method instead of comparing a
  stored `19.2s` timing.

  `docs/DOGFOOD.md` now builds exact immutable base/change SHAs together,
  alternates at least four adjacent pairs with both orders represented, records
  every source/item/byte scale and delta, separates filesystem and aibris
  application-cache state, rejects failed scans and unverifiable cold eviction,
  and treats a minimum manual series as `inconclusive` without a predeclared
  stability rule. #129 remains the owner of the synthetic harness and non-flaky
  budget.

  Final real-home warm verification ran eight scans with no clean command. All
  observations matched at **8 sources / 311 items / 23,342,632,843 B** while
  `codex-activity.json` stayed byte-identical. Paired deltas were **-0.02s,
  -0.76s, +1.59s, +1.49s**; median **+0.74s**, range **[-0.76s, +1.59s]**, and
  the result was correctly reported as **inconclusive** rather than promoted to
  a target. Base and change `-trimpath` binaries were byte-identical. Merged
  `main` passed race tests, build, vet, Linux/Windows/Darwin builds, diff check,
  and backlog doctor.
- 2026-07-29 19:33: Shaped #151 as relay request
  `req-20260729103342292` with two ordered leaves. L1
  `overlap-safety-kernel` preserves complete scan safety evidence, locks
  bidirectional live/undetermined overlaps, and carries every nested orphan
  revalidation obligation to a pre-mutation barrier. L2
  `overlap-provenance-accounting` depends on merged L1 and completes physical
  ownership, visible-row, audit, and human-receipt lineage. Historical real-home
  overlap counts remain observations; current same-session dry-runs and
  synthetic deletion fixtures are the verification contract.
- 2026-07-30 19:08: #151 L1 dispatched → PR #159 → hardened review (semantic
  PASS, round 4) → squash-merged as `d348ede`; #151 remains open for L2, and
  the run worktree plus local/remote branch were cleaned. All 15 Done Criteria,
  SHA-bound execution evidence, an independent Codex audit, and all four CI
  checks passed. The maintainer authorized force-finalization because the
  optional OpenCode advisory returned semantically valid low-confidence output
  without the newly required `severity` field, demoting an otherwise clean run
  to `escalated`; dev-relay #1122 tracks that transport failure.

  Merged `main` passed `git diff --check`, race tests, build, vet, and
  Linux/Windows/Darwin builds. Immutable binaries built from the final reviewed
  head `ef9b7fe` and merged `main` were byte-identical. Concurrent real-home
  `clean --dry-run --no-guide` scans both reported **212 planned items /
  1.6 GB** and agent-state **276 found / 209 eligible (232.4 MB) / 67
  protected**. Their sorted 212-target path sets were identical
  (`32482ec29a588cbfabae636bfdeccaa2ee6dca429c2c31268f0f1dbebc4842fe`);
  no safety refusal occurred. These current absolute counts have drifted with
  the live stores, so only the same-session equality is evidence that the merge
  changed no behavior.

  Two recovery-path follow-ups were also recorded: dev-relay #1119 now carries
  the live gate-placeholder reproduction, and #1123 tracks
  `recover-commit --dry-run` incorrectly returning `nothing_to_recover` when an
  existing PR is the recovery anchor.
- 2026-07-30 19:20: Started #151 L2 as relay run
  `issue-151-20260730102000000` on
  `issue-151-overlap-provenance-accounting`. `codexbar usage --json` reported
  Codex weekly usage at 36% (dashboard and code-review views 31%) versus Claude
  weekly usage at 88%, so Codex/Sol is the xhigh executor and primary reviewer;
  hardened advisory remains policy-driven.

  An independent read-only Codex/Sol-high planning audit confirmed the ten
  criteria are implementable without activating #115, #125, or #147, and the
  current race/build/vet/three-OS baseline passes. A persisted planner addendum
  chooses the conservative existing-contract interpretation: selectors never
  enlarge deletion scope; component state is
  `locked > selected > reviewable`; physical bytes belong only to the owner;
  canonical aliases never change the raw mutation target; plan-time refusals
  remain audit/preview evidence; and cancellation records completed obligations
  plus remaining `not-attempted` obligations while freeing zero bytes.
- 2026-07-30 21:05: #151 L2 dispatched → PR #160 → hardened review (primary
  semantic PASS, round 3) → squash-merged as `23f7b38`; #151 closed, Batch 3
  completed, and the run worktree plus local/remote branch were cleaned. The
  maintainer authorized force-finalization because the non-gating OpenCode
  advisory emitted invalid JSON after the final Codex review had passed all ten
  Done Criteria and both quality factors. Four subsequent public review threads
  were fixed and resolved at the final reviewed head `938fdae`; all four CI
  checks passed.

  Merged `main` passed race tests, build, vet, and Linux/Windows/Darwin builds.
  Its immutable binary ran only `clean --dry-run --no-guide` against the real
  home and planned the same **213 items / 1.6 GB** as the pre-merge binary.
  Agent-state remained byte-identical at **277 found / 210 eligible
  (246.1 MB) / 67 protected (429.4 MB)**. The overall scan moved from 318 to
  317 physical items, and protected/skipped from 105 to 104, solely because
  Relay reclaimed the completed run's protected worktree during merge cleanup;
  no files were removed by aibris.
- 2026-07-30 21:20: Started #140 as hardened relay run
  `issue-140-20260730121915000` on
  `issue-140-bounded-worktree-containers`. `codexbar usage --json` reported
  Codex weekly usage at 50% (dashboard and code-review views 31%) versus Claude
  weekly usage at 88%, so Codex xhigh is the executor and primary reviewer;
  independent Codex subagents supplied the discovery, invalid-metadata, and
  same-session measurement audits.

  The frozen contract uses a finite exact registry for known containers while
  preserving the existing generic depth-4 fallback. It treats every container
  unit as one physical mutation owner: any missing or malformed Git marker
  makes the whole owner visible but review-only as `plain-dir`, while I/O
  uncertainty remains partial provider evidence. Current same-session evidence
  is an observation, not a target: `fe8eb16` full-home scan finds zero
  superpowers rows, but a scoped scan finds two valid L2 members under one
  540,565,504-byte physical owner.
- 2026-07-31 01:10: #140 dispatched → PR #163 → hardened primary review
  (semantic PASS, round 6) → squash-merged as `8caa554`; #140 closed, Batch 4
  completed, and the run worktree plus local/remote branch were cleaned. The
  maintainer authorized force-finalization after the final Codex review passed
  all 12 Done Criteria and the hardened OpenCode advisory returned empty stdout
  twice after reviewer-swap exhaustion. The post-review Ubuntu failure exposed
  a deterministic audit-only path-alias bug hidden by macOS `/var` canonical
  aliases; an independent audit confirmed the three-line priority fix, its
  direct regression test, and no mutation-path effect. Ubuntu/macOS CI,
  release-build, and CodeRabbit all passed at the final PR head.

  Merged `main` passed race tests, build, vet, and Linux/Windows/Darwin builds.
  Same-session real-home dry-runs from immutable pre-merge `41cab28` and merged
  `8caa554` binaries both planned **213 items / 1.6 GB** and reported
  agent-state **277 found / 210 eligible (246.1 MB) / 67 protected
  (429.4 MB)**. The new container registry increased protected physical
  coverage from 319 to 362 owners while leaving planned and agent-state values
  exactly equal; all aibris real-home clean invocations used `--dry-run`.
- 2026-07-31 01:28: Shaped #142 as relay request
  `req-20260730162731396` with three ordered leaves and started L1
  `store-classification` as hardened run
  `issue-142-20260730162810830-b1fe8cda`. Read-only metadata corrected two
  audit assumptions: `computer-use` is an installed OpenAI application bundle,
  and Cursor `ai-tracking` contains tracked-file and conversation-summary
  schema rather than disposable telemetry alone. The fail-closed classification
  keeps packages/computer-use provider-free, makes tmp only a future
  direct-child candidate, permits generated-image retention selection only
  after #139 L1, and leaves live sqlite/ai-tracking databases inventory-only.
  No content bodies, SQLite row values, tracked file values, or image pixels
  were inspected.
- 2026-07-31 02:56: #142 L1 dispatched → PR #164 → hardened primary review
  (all ten Done Criteria and 10/10 evidence-to-policy traceability passed in
  round 5) → squash-merged as `228e0f9`; #142 remains open for L2/L3, and the
  run worktree plus local/remote branch were cleaned. The maintainer authorized
  force-finalization after the OpenCode adversarial lane exhausted its
  two-demotion budget and continued proposing future L2/L3 implementation
  details beyond this Markdown-only classification leaf. Genuine findings were
  incorporated through final head `28c3beb`, including the literal tmp
  direct-child boundary, upstream cooperation gate, database-family
  fail-closed rules, and recursive-unit versus non-traversing symlink deletion
  distinction. Exact-head race tests, build, vet, Linux/Windows/Darwin builds,
  and all four public checks passed.
- 2026-07-31: Froze #139 L1 as relay-ready request
  `req-20260730181100894` after two independent architecture stress-tests.
  L1 is Markdown-only: retention buckets are a non-additive projection, never
  executable `DebrisInfo` rows; exact closed UTC-month selection must resolve
  to an immutable member manifest; and #138 proof-based eligibility plus #151
  overlap hard locks remain authoritative. The contract also keeps #142
  SQLite/ai-tracking inventory-only until producer cooperation exists.
- 2026-07-31 04:09: #139 L1 → PR #165 → squash-merged as `2d9714a`;
  Batch 6 completed while #139 remains open for L2–L4. Three exact-head
  hardened primary rounds passed every Done Criterion, but the advisory lane
  exhausted its budget on OpenCode schema, Pi authentication, and Antigravity
  empty-output failures rather than a repository finding. Public review then
  found two genuine execution-contract gaps: deletion now binds the validated
  entry and physical identity to a non-following mutation while its fence is
  held, and a retention-selected plan can no longer widen into a broader
  recursive #138 cleanup. Both were fixed at `7397525`, independently reviewed
  with no remaining actionable finding, and all public threads were resolved.
  Ubuntu/macOS CI, release-build, and CodeRabbit passed; Relay removed the run
  worktree and local/remote branch.

  Merged `main` passed race tests, build, vet, and Linux/Windows/Darwin builds.
  Same-session real-home dry-runs from immutable pre-merge `4f6ad77` and merged
  `2d9714a` binaries produced identical normalized summaries: **213 items /
  1.6 GB** planned, with agent-state **277 found / 210 eligible (246.1 MB) /
  67 protected (429.4 MB)**. These are observations rather than baselines, and
  every real-home cleanup invocation used `--dry-run`.
- 2026-07-31 04:25: Revalidated #142 L2/L3 readiness after #139 L1 and marked
  the canonical issue `status:blocked`; the unchecked Batch 7 leaves remain
  incomplete. Codex 0.144.4 exposes heterogeneous `~/.codex/tmp` direct
  children, and upstream's `.lock` fences individual
  `arg0/codex-arg0*` session directories rather than freezing the tmp root or
  every writer before discovery. The generated-image source documents a path
  shape but persists no producer-version identity/manifest and exposes no
  cooperative writer fence. SQLite and Cursor `ai-tracking` likewise still
  lack the complete family registry and all-writer quiescence contract.
  Independent audits found no safe locally implementable #142 leaf, and no
  duplicate aibris issue exists.

  A separate audit found #139 L2 technically unblocked if it is kept strictly
  read-only: a non-additive Codex-session UTC-month projection with no selector,
  manifest, cleanup candidate, or executor. It does not depend on #142's tmp,
  generated-image, or database mutation proofs. Advancing Batch 8 ahead of the
  blocked Batch 7 would change the recorded sprint order, so dispatch awaits an
  explicit maintainer-approved reorder rather than silently weakening or
  bypassing the plan.
- 2026-07-31 04:29: Prepared but did not plan or dispatch #139 L2 as Relay
  request `req-20260730192929540`. The single frozen leaf,
  `l2-codex-sessions-retention-inventory`, has 22 Done Criteria covering a
  separate non-additive retention projection, bounded first-record metadata,
  UTC-month and orphan-subset aggregation, cache/JSON compatibility, cleanup
  non-interference, same-session accuracy, paired performance, and the full Go
  gate. Its escalation contract stops before `relay-plan` or dispatch until the
  maintainer explicitly approves advancing Batch 8 ahead of blocked Batch 7;
  selector, manifest, cleanup eligibility, executor, and mutation remain out
  of scope.
- 2026-07-31: [actor:codex] The maintainer explicitly approved advancing Batch 8 ahead of
  externally blocked Batch 7. #139 L2 is now in progress under the already
  frozen Relay request `req-20260730192929540`; its 22 Done Criteria and
  read-only, non-additive boundary remain unchanged
  [branch:issue-139-codex-sessions-retention-inventory].
- 2026-07-31 15:05: Parked #139 L2 with the frozen contract intact after the
  maintainer chose not to stop other active Codex work. Implementation and
  local verification are complete at `6b89b00`, but the run remains
  pre-publication and DC19–21 remain unverified because every real-home
  quiescence probe drifted. No PR was opened and nothing was merged. Batch 8
  L3/L4 stay dependent; execution may move to the independent #141 guided
  review batch while L2 awaits an offline measurement window.
- 2026-07-31 15:23: Started independent Batch 9 issue #141 after parking #139
  L2. Relay-ready froze one high-risk leaf with 17 Done Criteria: guided review
  admits every active worktree tool, preserves source/tool identity and every
  Git/overlap/preflight lock, keeps unavailable registered activity evidence
  locked, and makes tools with no activity source explicitly reviewable but
  never auto-recommended. Codex implementation is isolated in Relay run
  `issue-141-20260731062303100-927250e4`; publication is delayed until hardened
  internal review, and no PR or merge is authorized by this status change.
- 2026-07-31 17:51: Parked #141 pre-publication at pushed head `0a47323`.
  Implementation, exact-head race tests, build, vet, and all three release
  target builds pass. Independent review found two genuine ordering blind
  spots—age replans and the protected display could violate
  recommended/size/stable-key order—and both now have render-level regression
  coverage. The recovered OpenCode advisory also prompted one shared
  representative/activity comparator and an explicit `--tool=unknown` CLI
  test. Codex primary rounds 2–4 then passed all 17 Done Criteria and quality
  with zero issues.

  Relay remains `escalated` solely because no configured advisory adapter
  produced valid hardened evidence: Pi had no API credentials, OpenCode emitted
  an otherwise useful response with an invalid zero line number, Antigravity
  emitted no structured result, and the owner-approved fourth Cline attempt
  lacked a ClinePass subscription. The review cap was increased once from 3 to
  4 with audited owner approval and will not be extended again. No PR was
  opened and nothing was merged.
