# Protected-Content Retention Contract

This document freezes the read-only inventory contract for protected
AI-tool content. Retention is an authorization axis separate from debris
category risk and `agent-state` classification: it **adds no category,
classification, `DebrisInfo` kind, cleanup path, or `--risky` opt-in**.

The execution layer (retention selector, exact-member manifest, planner, and
executor) is **explicitly parked** for #139. This contract covers only a
bounded read-only aggregate inventory. Parked machinery is not revived by
merging this document; reviving it requires a new contract leaf.

## Stores, units, and bounded evidence

Each store owns exactly one bounded root. A provider may inspect only the
versioned metadata envelope and filesystem metadata needed to recognize
complete units; it may not broadly walk the surrounding tool home.

| Store ID | Exact bounded root | Bounded retention unit | Trusted primary timestamp |
| --- | --- | --- | --- |
| `codex-sessions` | `~/.codex/sessions` | One recognized fixed Codex session regular-file leaf. The leaf is both the unit anchor and its only content member unless a later producer-versioned layout explicitly registers bounded metadata companions. | The fixed session file's `Lstat.ModTime`. Date-shaped ancestors and timestamps found later in the transcript do not participate. |

Additional stores (`cursor-chats`, `claude-projects`, `gstack-projects`,
`relay-runs`, `codex-generated-images`) remain future provider work under the
same root/unit/timestamp discipline; they are not shipped with this contract.

A recognizer binds every supported producer version to the unit anchor and
exact permitted member set. Unknown versions, layouts, anchors, or companions
make the affected unit uncountable for orphan statistics (it still counts in
its month bucket). Directory mtime, date-shaped paths, encoded names, decoded
cwd guesses, content-body scans, and content hashes are forbidden timestamp or
identity fallbacks. Unusable timestamps put the unit in the visible `unknown`
bucket.

## UTC bucket identity

The only bucket identity is a UTC calendar month in `YYYY-MM` form. A unit is
a member of month `M` exactly when its effective timestamp is in the half-open
interval:

```text
[monthStartUTC(M), monthStartUTC(M + 1))
```

The `unknown` bucket, the current UTC month, and every future UTC month remain
visible in inventory. A month is closed only after its UTC end boundary has
passed. Year views are display-only roll-ups; a year, wildcard, range,
relative age, or local-time month is never an inventory or execution identity.

## Read-only retention projection

Retention inventory is a projection over physical inventory, not another set
of executable debris rows. It contains exactly one aggregate row per
`(store_id, bucket_id)`, including protected open and `unknown` buckets. Every
row carries:

- `store_id`
- `bucket_id`
- `unit_count`
- `member_count`
- `apparent_bytes`
- `orphaned_count`
- `orphaned_bytes`

`unit_count` is the number of bounded retention units. `member_count` is the
number of physical regular-file leaves owned by those units.
`apparent_bytes` is the sum of owned regular-file `Lstat.Size` values, counted
once per physical identity. It is neither allocated blocks nor a promise about
space that deletion will free. Orphan subset counts and bytes never increase
the row total.

Aggregate rows are never `DebrisInfo` values, never enter `summary.total_*`,
category totals, or tool totals, and are never executable. No member path,
session identifier, or transcript content appears in the projection or its
provider diagnostics. Existing `agent-state` owners (e.g. #138's
`claude-projects` coverage) remain the only mutation surface for that content.

## Codex orphan statistics are evidence only

Codex session orphan statistics reuse the shared recorded-cwd classifier on
each bounded session unit. The classifier reads only the producer-bounded
`cwd` metadata envelope:

- any usable cwd that still exists makes the unit `live`;
- `orphaned` requires every usable cwd to be proven absent;
- malformed, unreadable, ambiguous, or otherwise unusable evidence makes the
  result `undetermined`;
- a valid record with no cwd is not evidence that a cwd is absent;
- encoded names are never decoded into cwd guesses.

These statistics are a subset annotation on the aggregate row. They never emit
`EntryClassOrphaned`, never create an executable `DebrisInfo` row, and never
create default cleanup eligibility.

**No retention row authorizes default cleanup.** The merged #138 behavior
remains unchanged: proof-based orphaned Claude and Cursor `agent-state` owners
remain eligible without an age gate, while `live` and `undetermined` owners
remain protected. The inventory itself is read-only: it never selects,
prepares, or mutates members. Partial inventory (permission or I/O failures
inside the store) degrades the retention section to `partial: true` without
affecting debris results or cleanup authorization.

## Absolute exclusions and blocked stores

Installed content is not debris or retention content. At minimum, the binary
exclusion registry contains:

- `~/.codex/packages`
- `~/.codex/computer-use`
- `~/.codex/plugins`
- `~/.claude/skills`
- `~/.cursor/extensions`

Known installed content such as `~/.claude/plugins` remains excluded as well.
Excluded paths are absent from every provider, ordinary inventory, and
retention projection. Codex SQLite and Cursor `ai-tracking` are not inventory
stores; they remain out of scope until separate contracts provide
producer-versioned identity and a complete database-family registry.

## Verification gate

Provider work must not reinterpret the point-in-time real-home counts, sizes,
or timings in [DOGFOOD.md](DOGFOOD.md) as targets. The inventory is a
point-in-time snapshot; store drift between scans is expected and harmless
because the inventory never mutates.
