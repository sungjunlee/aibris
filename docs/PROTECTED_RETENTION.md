# Protected-Content Retention Contract

This document freezes the fail-closed contract for future protected-content
inventory, selection, planning, and execution. It is an authorization axis
separate from debris category risk and `agent-state` classification. Retention
does not add a category, classification, `DebrisInfo` kind, or default cleanup
path, and `--risky` is not retention consent.

Everything described here as a provider, retention projection, selector,
manifest, or executor is **planned and unshipped**. The current CLI flags and
JSON schema remain those documented in [SPEC.md](SPEC.md) and
[JSON_SCHEMA.md](JSON_SCHEMA.md).

## Stores, units, and bounded evidence

Each future store owns exactly one bounded root. A provider may inspect only
the versioned metadata envelope and filesystem metadata needed to recognize
complete units below; it may not broadly walk the surrounding tool home.

| Store ID | Exact bounded root | Bounded retention unit | Trusted primary timestamp |
| --- | --- | --- | --- |
| `codex-sessions` | `~/.codex/sessions` | One recognized fixed Codex session regular-file leaf. The leaf is both the unit anchor and its only content member unless a later producer-versioned layout explicitly registers bounded metadata companions. | The fixed session file's `Lstat.ModTime`. Date-shaped ancestors and timestamps found later in the transcript do not participate. |
| `cursor-chats` | `~/.cursor/chats` | One producer-versioned chat envelope anchored by one recognized primary regular-file leaf, with an exact complete member set bounded to that chat entry. | The later of a trusted producer terminal/update timestamp in the bounded metadata envelope and the primary leaf's `Lstat.ModTime`; without that producer timestamp, the primary leaf's `Lstat.ModTime`. |
| `claude-projects` | `~/.claude/projects` | One producer-versioned session envelope inside one immediate project entry, anchored by one recognized transcript primary regular-file leaf and its exact registered metadata companions. The encoded project-entry name is not identity. | The later of a trusted producer terminal/update timestamp in the bounded metadata envelope and the primary leaf's `Lstat.ModTime`; without that producer timestamp, the primary leaf's `Lstat.ModTime`. |
| `gstack-projects` | `~/.gstack/projects` | One producer-versioned project-run envelope inside one immediate project entry, anchored by one recognized primary regular-file leaf and its exact registered metadata companions. | The later of a trusted producer terminal/update timestamp in the bounded metadata envelope and the primary leaf's `Lstat.ModTime`; without that producer timestamp, the primary leaf's `Lstat.ModTime`. |
| `relay-runs` | `~/.relay/runs` | One producer-versioned run envelope anchored by one recognized run-manifest primary regular-file leaf and its exact registered metadata companions. | The later of a trusted producer terminal/update timestamp in the bounded metadata envelope and the primary leaf's `Lstat.ModTime`; without that producer timestamp, the primary leaf's `Lstat.ModTime`. |
| `codex-generated-images` | `~/.codex/generated_images` | One recognized generated-image regular-file leaf. This is a downstream extension store and remains unshipped with the other retention stores. | The generated-image leaf's `Lstat.ModTime`, unless a later producer-versioned bounded metadata envelope supplies a terminal/update timestamp, in which case the later instant is used. |

A future recognizer must bind every supported producer version to the unit
anchor and exact permitted member set. Unknown versions, layouts, anchors, or
companions make the affected unit incomplete and non-executable. A unit's
filesystem timestamp is anchored to its primary regular-file leaf; directory
mtime is never a substitute.

Only shallow, bounded metadata is admissible. Directory mtime, date-shaped
paths, encoded project names, decoded cwd guesses, content-body scans, and
image or content hashes are forbidden timestamp or identity fallbacks.
Unreadable, zero, out-of-range, or otherwise unusable timestamps put the unit
in the visible `unknown` bucket.

## UTC bucket identity

The only bucket identity is a UTC calendar month in `YYYY-MM` form. A unit is a
member of month `M` exactly when its effective timestamp is in the half-open
interval:

```text
[monthStartUTC(M), monthStartUTC(M + 1))
```

Year views are display-only roll-ups. A year, wildcard, range, relative age, or
local-time month is never a selector or execution identity.

The `unknown` bucket, the current UTC month, and every future UTC month remain
visible in inventory but protected and unselectable. A month is closed only
after its UTC end boundary has passed. Clock ambiguity or an unusable reference
time fails closed.

## Non-additive retention projection

Future retention inventory is a projection over physical inventory, not
another set of executable debris rows. It contains exactly one aggregate row
per `(store_id, bucket_id)`, including protected open and `unknown` buckets.
Every row carries at least:

- `store_id`
- `bucket_id`
- `unit_count`
- `member_count`
- `apparent_bytes`
- `orphaned_count`
- `orphaned_bytes`
- `selectable`
- `blocked_reason`

`unit_count` is the number of bounded retention units. `member_count` is the
number of physical regular-file leaves owned by those units.
`apparent_bytes` is the sum of owned regular-file `Lstat.Size` values, counted
once per physical identity. It is neither allocated blocks nor a promise about
space that deletion will free.

Every Codex sessions row exposes `orphaned_count` and `orphaned_bytes` as the
orphaned subset described below, using zero when the subset is empty. Other
stores report zero for those fields until an equally explicit store contract
exists. Subset counts and bytes never increase the row total.

Aggregate rows are never `DebrisInfo` values and are never executable. Member
details belong to a prepared execution manifest, not to the aggregate
projection. Aggregate and member values must never be added together. Existing
`summary.total_count`, `summary.total_size`, category totals, and tool totals
continue to count each physical owner once; a retention view of the same
content contributes no second physical count or byte total. This is especially
important for `claude-projects`, whose underlying content may also be covered
by the existing proof-based `agent-state` owner.

One canonical member has exactly one registry owner. Hard-link aliases are
deduplicated with available device/file identity (or the platform-equivalent
stable identity). If complete identity and ownership cannot be proved, the
affected unit remains visible but non-executable, its unprovable bytes receive
no owned-byte or freed-byte credit, and `blocked_reason` explains the
fail-closed result.

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

**No retention-only row authorizes default cleanup.** The merged #138 behavior
remains unchanged: proof-based orphaned Claude and Cursor `agent-state` owners
remain eligible without an age gate, while `live` and `undetermined` owners
remain protected. Reversing that behavior requires a separate breaking leaf;
it is not part of this contract.

## Explicit selection

The only future retention selector token is:

```text
<store_id>@<YYYY-MM>
```

The planned repeatable CLI spelling is
`--retention-bucket <store_id>@<YYYY-MM>`. It is specified here for future
implementation and is **not a current CLI flag**.

A selector is accepted only for a known store and an exact closed UTC month
from a fresh, complete inventory. A bucket containing an incomplete unit is
unselectable. Wildcards, ranges, years, `unknown`, the current or a future
month, unknown stores, and partial inventories are rejected. Here
`selectable` authorizes exact-member preparation, not execution; complete units
may still be protected by proof-based hard locks in the manifest. `--risky`,
`--age`, `--category`, `--tool`, and `--force` are never substitute retention
opt-ins. After a valid explicit selection, `--force` may only skip
confirmation; it cannot unlock a unit or weaken revalidation.

The future L4 command path must either normalize a mixed retention and
classic/guided selection into one plan with one preview, confirmation, and
receipt, or reject the combination before preview. It must never execute two
independent mutation paths.

## Exact-member preparation

Selecting an aggregate bucket authorizes only preparation. Before dry-run or
confirmation, every selected bucket is resolved from a fresh, complete
inventory into one immutable, exact-member manifest. The manifest records its
own schema name and version and, for every selected bucket and unit:

- store ID, bucket ID, and exact bounded root;
- stable unit key and physical-owner key;
- raw relative path and canonical relative path beneath the bounded root;
- platform physical identity;
- entry type;
- `Lstat.Size`;
- `Lstat.ModTime` and the effective timestamp;
- authorization or protection disposition and its blocking reason.

The complete manifest includes hard-locked members as non-mutating evidence so
a bucket selection cannot silently omit a locked unit. Incomplete units make
the aggregate bucket unselectable and cannot enter manifest preparation. Only
members explicitly authorized in that immutable manifest can reach execution.
The manifest contains no transcript body, SQLite row, generated-image pixel,
tracked-file value, or content hash.

Aggregate rows themselves are never executed. A manifest cannot be prepared
from a cached, partial, or drifting retention inventory. Symlinks, root
escapes, unsupported entry types, missing physical identities, unknown
members, and incomplete units remain protected.

## Execution and revalidation

Retention execution must never recursively delete a bucket directory or store
root. It may unlink only exact manifested, authorized regular-file members.
Files created after preview are unmanifested and remain untouched. After
member removal, a manifested member's ancestors below the store root may be
removed only with non-recursive `rmdir` after each is empty; the store root is
never removed. Symlinks are neither followed nor unlinked by retention
execution.

Before the first mutation, execution revalidates the whole manifest:

- store registry ownership and physical-owner uniqueness;
- exact complete unit and member sets;
- bucket membership and effective timestamps;
- raw and canonical containment beneath the exact bounded root;
- entry type, physical identity, size, and mtime;
- every proof-based hard lock and deletion-time obligation.

Any preflight drift refuses the whole plan before mutation. Immediately before
each unit mutation, execution checks that unit's containment, exact identity,
type, size, `Lstat.ModTime`, and effective timestamp again. Drift discovered
after earlier units were removed stops the affected unit and every remaining
unit, preserves all unmanifested content, returns non-zero, and emits a
truthful partial receipt. Receipts count bytes once per physical owner and
credit only owners actually removed; incomplete ownership or a partially
removed unit receives zero freed-byte credit.

## Proof and hard-lock precedence

Existing proof-based safety always outranks retention selection. In
particular, the #151 containment contract locks a retention unit when a `live`
or `undetermined` `agent-state` owner is its ancestor, descendant, or exact
alias. An orphaned outer owner retains every existing deletion-time child
revalidation obligation. Explicit retention selection cannot weaken #138,
#151, or any other hard lock.

If existing #138 cleanup owns the same physical content, it continues through
its existing proof-based plan and executor rather than through a retention
aggregate. This preserves current eligibility while still forbidding
retention execution from recursively deleting a bucket or store root.

## Absolute exclusions and blocked stores

Installed content is not debris or retention content. At minimum, the binary
exclusion registry contains:

- `~/.codex/packages`
- `~/.codex/computer-use`
- `~/.codex/plugins`
- `~/.claude/skills`
- `~/.cursor/extensions`

Known installed content such as `~/.claude/plugins` remains excluded as well.
Excluded paths are absent from every provider, ordinary inventory, retention
projection, exact-member manifest, selector, and cleanup surface. A selector
that names or resolves into installed content is invalid.

Codex SQLite and Cursor `ai-tracking` are not retention-executable stores.
They remain protected and inventory-only in planning until separate contracts
provide producer-versioned identity, producer-cooperative exclusion and
quiescence, a complete database-family registry, and safe atomic family
mutation. Merging this Markdown contract alone does not unblock issue #142 L2
or L3.

## Future implementation and verification gate

Provider, planner, and executor work in L2-L4 must preserve this contract and
must not reinterpret the point-in-time real-home counts, sizes, or timings in
[DOGFOOD.md](DOGFOOD.md) as targets. Provider performance must use that
document's complete same-session paired-delta protocol: immutable base and
change binaries, one controlled session and application-cache state,
alternating adjacent pairs, per-run scale, drift rejection, and
change-minus-base reporting for each cache condition.
