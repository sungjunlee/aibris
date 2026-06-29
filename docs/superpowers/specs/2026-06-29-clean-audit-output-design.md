# Clean Audit Output Design

Date: 2026-06-29
Status: Approved design

## Context

`aibris` already has safe cleanup primitives: scan roots are home-scoped,
active worktrees are protected by default, risky categories require `--risky`,
and `clean` previews targets before deletion. The remaining product gap is
human trust. The current `clean` output can show progress and a target plan, but
it does not read like a complete cleanup audit. Users can still wonder what was
scanned, why some items are protected or skipped, and exactly what the deletion
plan is based on.

This design improves the human `aibris clean` output. It does not change
cleanup policy, deletion behavior, or `scan --json` compatibility.

## Goals

- Make `aibris clean` read as a cleanup session audit.
- Show scan source, policy, candidate totals, protected/skipped totals, and
  category-level reasons before deletion.
- Make `clean --dry-run` and real `clean` share the same audit shape.
- Add reason text to human target rows so each candidate has an obvious basis.
- Keep the existing scanner, cleaner, and JSON contracts stable.

## Non-Goals

- Build a general macOS cleaner UI.
- Clone the full `mo clean` category taxonomy.
- Change `internal/types.DebrisInfo` or the JSON schema.
- Change the `cleaner.Execute` API in this pass.
- Add machine-readable execution receipts.
- Rework scan performance or size estimation.

## User-Facing Output

`aibris clean` should be organized as a session audit:

```text
clean
  roots  ~

  scanning node_modules
  scanning build-cache
  found    build-cache    3 items   2.1 GB
  found    node_modules   11 items   8.4 GB

  policy  age>=7d, risky=false, active-worktrees=protected
  scan    live

scan summary
  scanned    7 sources   23 items   14.2 GB
  eligible   9 items     6.8 GB
  protected/skipped 14 items   7.4 GB

by category
  category       found        eligible     protected/skipped       main reason
  node_modules   11  8.4 GB   7  5.8 GB   4  2.6 GB               younger than 7d
  worktree        8  3.5 GB   2  1.0 GB   6  2.5 GB               active worktree protected
  build-cache     3  2.1 GB   3  2.1 GB   0  0 B                  eligible for cleanup
  ai-logs         1  200 MB   0  0 B      1  200 MB               requires --risky

clean plan
  mode     dry-run
  targets  9 items   6.8 GB

targets
      size  category      name          project      age/status      action        reason
    1.8 GB  node_modules  dashboard     -            24d             remove-path   dependency directory; can be reinstalled
```

When there are no deletion candidates, the command should still print the audit
sections that explain why:

```text
clean
  roots  ~
  policy  age>=7d, risky=false, active-worktrees=protected
  scan    cached, 8s old

scan summary
  scanned    7 sources   4 items   3.2 GB
  eligible   0 items     0 B
  protected/skipped 4 items   3.2 GB

by category
  category       found        eligible     protected/skipped       main reason
  worktree        1  96.0 MB  0  0 B      1  96.0 MB              active worktree protected
  build-cache     3  3.1 GB   0  0 B      3  3.1 GB               younger than 7d

No items to clean.
```

After real deletion, the receipt remains intentionally small. It reports the
requested target count and total freed bytes without claiming precise per-item
success counts:

```text
cleanup receipt
  targets    9 items
  freed      6.8 GB
  protected/skipped 14 items   7.4 GB
```

If execution returns an error, existing stderr detail remains the source of
truth. This pass must not print a precise failed item count because
`cleaner.Execute` does not return structured per-item results.

## Architecture

Keep the existing responsibilities:

- `internal/scanner`: discover debris and aggregate scan results.
- `internal/cleaner`: filter and execute cleanup policy.
- `cmd`: parse flags and render human output.

Add a presentation-only audit builder in `cmd`:

```text
ScanResult
  |
  v
cleaner.Filter(items, opts) -> targets
  |
  v
cmd/buildCleanAudit(result.Worktrees, targets, opts)
  |
  +-- category rows: found / eligible / protected-or-skipped / main reason
  +-- totals: found / eligible / protected/skipped
  +-- target rows: existing clean plan plus reason
  |
  v
cmd/printCleanAudit(...)
```

Suggested display-only types:

```go
type cleanAudit struct {
	Totals     cleanAuditTotals
	Categories []cleanAuditCategory
}

type cleanAuditCategory struct {
	Category      types.Category
	FoundCount    int
	FoundSize     int64
	EligibleCount int
	EligibleSize  int64
	BlockedCount  int
	BlockedSize   int64
	MainReason    string
}
```

The implementation may choose equivalent names for these display-only fields,
but the boundary is fixed: audit state belongs in `cmd`, not in persisted scan
data.

## Reason Classification

Reason text should follow cleanup policy order so the human audit explains the
same decisions that `cleaner.Filter` makes.

Priority:

1. outside category or tool filter
2. risky category requires `--risky`
3. active worktree protected
4. younger than the configured age
5. path no longer exists
6. eligible for cleanup

Candidate row reasons should reuse existing presentation helpers such as
`itemReason(w)` where possible. Category `main reason` should be the dominant
blocked reason by size, with count as the tie-breaker. If all items in a
category are eligible, the main reason can be `eligible for cleanup`.

## Cache And Scan Source

`clean` should show whether it used a recent scan cache or a live scan:

- `scan    live`
- `scan    cached, 8s old`

Fresh scan cache reuse remains valid only when roots, schema, and freshness
match. Cached items must still pass the existing path existence check before
presentation and deletion. Items removed by that check should be counted as
skipped with `path no longer exists` in the audit.

## Error Handling

Scanner provider errors keep the existing stderr format:

```text
scan:<tool>:<error>
```

Provider errors should remain visible through existing stderr output. This pass
must not add provider error storage to `ScanResult`.

Deletion errors remain reported by `cleaner.Execute`. The final receipt should
not claim precise per-item success or failure until the execution API can return
structured item results.

## Testing

Add focused `cmd` tests around the human clean audit contract:

- `clean --dry-run` prints `policy`, `scan summary`, `by category`,
  `clean plan`, `targets`, and `reason`.
- active worktrees are counted as protected by default.
- `--include-active-worktrees` moves active worktrees into eligible targets.
- risky categories show `requires --risky` unless `--risky` is set.
- age-blocked items show a younger-than-age reason.
- cached scan usage is visible as the scan source.
- zero-candidate dry-runs still print audit summary and category reasons.
- `scan --json` remains valid JSON and does not include human audit text.

Run the full verification set after implementation:

```bash
go test ./...
go build ./...
go vet ./...
```

## Documentation

Update narrowly:

- `README.md`: replace clean examples with audit-shaped output.
- `docs/SPEC.md`: describe the human `clean` audit contract.
- `docs/DOGFOOD.md`: add one real local transcript after implementation.
- `docs/SCAN_CLEAN_IMPROVEMENT_PLAN.md`: mark the audit-output work as the
  active follow-up design.

## Formatting Contract

Tests should assert stable section names and key phrases, not exact whitespace.
The durable contract is the presence of audit sections, counts, sizes, and
reason text. Column widths should be chosen during implementation based on
current terminal readability and existing command tests.
