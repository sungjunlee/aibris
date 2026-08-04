# perfharness — offline four-pair measurement for #139 L2

An offline measurement harness implementing the **frozen #139 L2 four-pair
performance + correctness/A-B protocol**. It is the deterministic-input layer
that issue **#129** owns: it produces reproducible evidence about the
Codex-sessions retention projection without modifying the inventory-bearing
stores of a live home, so that the
eventual real-home measurement (the still-open Done Criteria **DC19-21**) can be
short and low-risk.

This tool **only produces evidence**. It does **not** close DC19-21, does
**not** publish or merge anything, and leaves the **#139 L2 park in effect**.

## What it does

1. Builds two **immutable** binaries via `git archive <ref> | tar -x` +
   `go build -trimpath`, and records each binary's SHA-256. The base is (by
   default) the merge-base of the change ref and `main`; the change is the
   feature ref. `git archive` is a read-only export — the source ref is **never
   checked out, rebased, or mutated**, so a frozen branch can serve as the
   change input safely.
2. Materializes a **deterministic synthetic agent home**: a
   `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` store with controlled apparent
   sizes, fixed leaf mtimes (which set the UTC `YYYY-MM` retention bucket), a
   configurable live/orphan recorded-cwd split, plus an auxiliary `node_modules`
   dir so the base binary has a non-empty existing inventory.
3. Warms both binaries, then runs an **alternating adjacent four-pair series**
   (`base→change`, `change→base`, …) of `scan --root <home> --json` under a
   fixed `env -i`-style environment, timing each with wall-clock `real`.
4. **Drift rejection**: a pair is accepted only if each binary's inventory
   signature is byte-identical across all its appearances, the home input
   fingerprint is stable across the run, and neither scan is partial/non-zero.
   On a frozen synthetic home drift is zero by construction, so this validates
   the harness mechanics; on a real home it is the quiescence guard.
5. **Correctness A/B**: asserts the change is *additive and non-interfering* —
   the existing inventory (`worktrees`+`summary`) is byte-identical between base
   and change, and the retention projection is present only on the change.
6. Reports `change-minus-base` per pair with median/range. **Without a
   predeclared threshold the series is reported `inconclusive`** (an observation,
   not a pass/fail), exactly as the frozen protocol requires; `#129` owns the
   non-flaky threshold.

## Non-flaky threshold verdict

A regression is declared only when **all** of these hold, so a single noisy
outlier pair cannot trip CI:

1. correctness A/B passes and the series is drift-free;
2. at least `-min-pairs` (default 3) drift-free pairs are accepted;
3. the median `change-minus-base` exceeds `-threshold`; **and**
4. at least `quorum` (default 0.67) of the *accepted* pairs individually exceed
   `-threshold` (the majority guard).

Note on the majority guard: because the median is the **high-median** (upper
middle element) and pairs count strictly above the threshold, at the default
four-pair configuration (accepted count ∈ {3, 4}) a median above the
threshold already implies the quorum is met — the high-median alone prevents a
single outlier from tripping CI. The `-quorum` guard only becomes binding when
`-pairs` is raised above 5, where it additionally requires the regression to be
broad (a majority of accepted pairs) rather than concentrated. This is a
deliberate anti-flake vs. false-negative trade-off: with `acceptedN ≥ 6`, a
regression that affects only half the pairs is reported as `no regression …
treated as noise`.

If the median exceeds the threshold but the quorum is not met, the verdict is
`no regression … treated as noise`. With fewer than `-min-pairs` accepted pairs
the verdict is `inconclusive`. Every report is labelled with its `platform`
(`GOOS/GOARCH`), so baselines can be recorded and compared per OS (macOS vs
Linux).

## What it does NOT do

- It does **not** verify the real-home Done Criteria (DC19-21). Those require a
  quiescent **real** home and the real-home invariants (e.g. the historical
  `81 orphaned / 44 live / 11 undetermined`); a synthetic home cannot stand in
  for them.
- It never runs `clean`. It only runs read-only `scan`.
- It does **not** modify the inventory-bearing stores (sessions, `node_modules`,
  caches, agent state). Note that `aibris scan` writes its own last-scan and
  codex-activity caches under the home's cache dir (`<home>/Library/Caches/aibris/`
  on macOS, `<home>/.cache/aibris/` on Linux) as a normal scan side-effect; with
  `-home` this lands inside the measured home. These cache paths are excluded
  from the input fingerprint, so they neither affect measurement validity nor
  trigger self-inflicted drift.

## Usage

Run from anywhere inside the aibris repo:

```sh
# Fast synthetic smoke run (tiny home, 2 pairs), printing a Markdown report.
go run ./tools/perfharness --quick --pairs 2

# Larger synthetic run with a machine-readable report.
go run ./tools/perfharness --pairs 4 --json-out report.json --md-out report.md

# Measure an EXISTING home (the real-home workflow). Run only during a quiet
# window; the input-fingerprint drift check rejects pairs if the home changes.
go run ./tools/perfharness --home "$HOME" --pairs 4 --md-out real-home.md
```

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-repo` | git toplevel | aibris repo root (source for `git archive`) |
| `-base` | merge-base of `-change` and `main` | base git ref |
| `-change` | `issue-139-codex-sessions-retention-inventory` | change git ref |
| `-pairs` | `4` | number of adjacent base/change pairs |
| `-threshold` | `0` | predeclared regression threshold for the median `change-minus-base` (`0` ⇒ report inconclusive) |
| `-min-pairs` | `3` | minimum drift-free pairs required before a pass/fail threshold verdict is issued |
| `-quorum` | `0.67` | fraction of accepted pairs that must individually exceed `-threshold` for a regression (the non-flaky majority guard) |
| `-home` | (unset) | measure an existing home instead of generating a synthetic one |
| `-quick` | off | tiny synthetic home for a fast smoke run |
| `-months` | `2024-01..2024-06` | comma-separated UTC month buckets (synthetic) |
| `-files-per-month` | `40` | rollout leaves per month (synthetic) |
| `-min-bytes` / `-max-bytes` | `512` / `4096` | apparent-byte range per rollout (synthetic) |
| `-live-every` | `3` | one live recorded cwd per N rollouts; `0` ⇒ all orphaned (synthetic) |
| `-node-modules-files` | `3` | files in the auxiliary node_modules dir; `<=0` omits it (synthetic) |
| `-workdir` | a temp dir | working dir for exported trees, binaries, and the synthetic home |
| `-keep` | off | keep the working dir after the run |
| `-md-out` / `-json-out` | (unset) | write the Markdown / JSON report to a path (Markdown also prints to stdout) |

Synthetic-home flags are ignored when `-home` is set.

## Files

- `synthhome.go` — deterministic synthetic-home generator.
- `schema.go` — black-box `scan --json` parser + canonical, order-insensitive,
  number-preserving inventory/retention signatures.
- `build.go` — immutable binary builder (`git archive` + `go build -trimpath` +
  SHA-256).
- `scan.go` — controlled `scan` runner + deterministic tree hashing (input
  fingerprint excludes the volatile cache; cache identity captured separately).
- `protocol.go` — four-pair orchestration, drift rejection, correctness A/B,
  inconclusive-by-default verdict.
- `report.go` — Markdown + JSON rendering.
- `main.go` — CLI wiring.

The package imports no aibris `internal/` package, so it compiles on `main`
independently of the (unpublished) retention provider and can build a base
binary from a tree that predates it. Heavy build/scan integration tests are
gated behind `PERFHARNESS_INTEGRATION=1`.
