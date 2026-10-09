# perfharness — offline four-pair measurement for #139 L2

As of 2026-10-07, this tool maintains the historical **#139 L2 four-pair
retention A/B protocol**, including synthetic fixtures, drift rejection,
threshold verdicts, isolated subprocess environments, and run-owned temporary
directory cleanup. It is not a general full-home performance budget or a
comparison tool for arbitrary current releases.

The frozen A/B contract requires a base **without** retention and a change
**with** additive retention, with identical existing inventory. Current
releases already include the read-only projection: comparing two such refs
fails the additive check by design. The historical default change ref may be
absent in a fresh clone; measurements need explicit locally available
`-base` and `-change` refs satisfying that protocol.

The current read-only inventory contract is maintained in
[PROTECTED_RETENTION.md](../../docs/PROTECTED_RETENTION.md); retention execution
remains parked. Synthetic results do not close historical real-home Done
Criteria DC19-21 or authorize cleanup, publication, or a merge.

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

Run from the repository root; this is a separate Go module:

```sh
# Inspect options without scanning or requiring historical refs.
go -C tools/perfharness run . --help

# Synthetic protocol, lifecycle, and environment regression tests.
go -C tools/perfharness test ./...

# Optional immutable build + scan smoke test; all inputs are temporary.
PERFHARNESS_INTEGRATION=1 go -C tools/perfharness test ./... -run '^TestBuildBinaryIntegration$' -count=1
```

For a historical A/B experiment, use `go -C tools/perfharness run .` with
explicit `-base` / `-change` refs and `-quick` for a small synthetic input.
Omitting `-home` generates the synthetic home. Report output paths should be
inside a disposable directory. The `-home` option is reserved for a separately
reviewed, quiescent existing-home experiment; it is not an onboarding step.

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
| `-workdir` | system temp directory | parent for a unique run temp directory containing exported trees, binaries, and the synthetic home; the parent is never removed |
| `-keep` | off | preserve only the run temp directory and print its path, including on error |
| `-md-out` / `-json-out` | (unset) | write the Markdown / JSON report to a path (Markdown also prints to stdout) |

Synthetic-home flags are ignored when `-home` is set.

Each run creates its own `aibris-perfharness-*` child directory with
`MkdirTemp`, under `-workdir` (creating the parent if needed) or the system
temp directory. Without `-keep`, only that child is removed on success or
error; existing parent files and sibling directories are preserved. If child
creation fails, the harness returns an error without removing the parent.
With `-keep`, the child is retained and its path is printed on success or error.

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

The executable imports no aibris `internal/` package and can build a base
binary from a tree that predates retention. The module has no dependency on
the root module; tests isolate HOME with a small local helper. Heavy
build/scan integration tests are gated behind `PERFHARNESS_INTEGRATION=1`.
