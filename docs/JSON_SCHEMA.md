# aibris JSON Output Schema

`aibris scan --json` outputs the following JSON structure. The output is
versioned; the top-level `schema_version` tells consumers which contract to
branch on.

## Versioning

- `schema_version` is `1` today. Consumers must treat an unknown (newer)
  `schema_version` as unsupported and stop rather than assume the shape.
- JSON compatibility and migration rules are defined by the
  [0.x compatibility and deprecation policy](COMPATIBILITY.md).
- The canonical all-debris array is `items`; it represents every debris
  category.
- The historical field name `worktrees` is retained as a **0.x compatibility
  alias** and mirrors `items` exactly. It exists so existing 0.x consumers do
  not break and is retained throughout 0.x. New consumers should read `items`.

The installed/regenerable/protected terms used by the issue #142 planning
taxonomy are not JSON fields or values. They do not extend `category` or
`classification`. This document describes only the shipped schema.

`aibris clean --dry-run --json` ships a separate versioned `clean_plan`
document. Non-dry-run `aibris clean --json` executes the plan built in the
current process and emits one versioned `clean_receipt` document. Plans and
receipts are not accepted as external execution inputs; there is no replay
command or persistent receipt store.

Phase-1 cleanup fails closed before encoding a `clean_plan` when the scan is
partial. Consequently, every emitted `clean_plan` has `evidence.complete: true`;
the partial-scan JSON described above applies to `scan --json`, not to a
cleanup plan.

The protected-content retention surface is frozen in
[PROTECTED_RETENTION.md](PROTECTED_RETENTION.md). It ships as an additive
read-only top-level `retention` object (see below) and is never an aggregate
rows disguised inside the historical `worktrees` array.

A complete scan keeps the established successful JSON shape below. If one or
more providers fail while other results remain usable, the command adds
`"partial": true` and a `provider_errors` array, prints the JSON document, and
exits with status 1. Consumers must treat the absence of `partial` as complete
and must not use a partial inventory as cleanup authorization.

## Top-level structure

```json
{
  "schema_version": 1,
  "items": [
    {
      "tool": "codex",
      "category": "worktree",
      "id": "abc123",
      "project": "my-project",
      "source": ".codex",
      "path": "/Users/user/.codex/worktrees/abc123",
      "size": 102400,
      "mod_time": "2026-05-25T12:00:00Z",
      "status": "orphaned",
      "risk": "low",
      "reason": "orphaned worktree; parent repo metadata missing",
      "cleanup_kind": "remove-path",
      "cleanup_command": []
    }
  ],
  "worktrees": [
    {
      "tool": "codex",
      "category": "worktree",
      "id": "abc123",
      "project": "my-project",
      "source": ".codex",
      "path": "/Users/user/.codex/worktrees/abc123",
      "size": 102400,
      "mod_time": "2026-05-25T12:00:00Z",
      "status": "orphaned",
      "risk": "low",
      "reason": "orphaned worktree; parent repo metadata missing",
      "cleanup_kind": "remove-path",
      "cleanup_command": []
    }
  ],
  "summary": {
    "total_count": 42,
    "total_size": 52428800,
    "by_category": {
      "worktree": { "count": 10, "size": 10240000 },
      "node_modules": { "count": 5, "size": 20971520 }
    },
    "by_tool": {
      "codex": { "count": 8, "size": 8192000 },
      "claude": { "count": 2, "size": 2048000 }
    }
  }
}
```

Partial scans add only these top-level fields:

```json
{
  "partial": true,
  "provider_errors": [
    {
      "tool": "codex",
      "message": "permission denied"
    }
  ]
}
```

Each provider error contains the failed provider name and its related error
message. Unrelated successful providers still contribute items and summary
counts. Cancellation is a hard failure: it does not emit a usable partial
result.

The top-level `retention` object is present on every scan:

```json
{
  "retention": {
    "buckets": [
      {
        "store_id": "codex-sessions",
        "bucket_id": "2026-07",
        "unit_count": 12,
        "member_count": 12,
        "apparent_bytes": 1048576,
        "orphaned_count": 3,
        "orphaned_bytes": 262144
      }
    ],
    "partial": false,
    "provider_errors": []
  }
}
```

`retention` is a non-authorizing read-only projection: its values never enter
`summary`, `total_count`, or `total_size`, never create cleanup candidates, and
a retention-local partial state (`retention.partial: true`) does not set the
top-level `partial` flag or change the exit status.

## Fields

### `schema_version`

| Field | Type | Description |
|-------|------|-------------|
| `schema_version` | integer | Top-level contract version. `1` today; unknown values are unsupported. |

### `items` array (canonical)

`items` is the canonical all-debris array. It contains one entry per debris
item from every category (worktrees, caches, `node_modules`, logs, agent
state). Its field set is identical to `worktrees`.

### `worktrees` array (0.x compatibility alias)

`worktrees` is the historical 0.x array name and is retained as a documented
compatibility alias. It is byte-for-byte identical to `items`. Consumers should
read `items`; the `worktrees` field is scheduled for removal after the 0.x
compatibility period.

It contains debris items from every category. Consumers should treat it as an
item list, not as a worktree-only list.

| Field | Type | Description |
| ------- | ------ | ------------- |
| `tool` | string | Tool name (`codex`, `claude`, `unknown`, `cursor`, `windsurf`, `node_modules`, `build-cache`, `pip-cache`, `ai-logs`). Generic worktree owners may remain `unknown`. |
| `category` | string | Debris category (`worktree`, `node_modules`, `build-cache`, `other-cache`, `agent-state`, `ai-logs`). Cursor entries under `~/.cursor/projects` use `agent-state`, not `ai-logs`. |
| `id` | string | Unique identifier (hash, directory name, or cache key) |
| `project` | string | Project name if detectable, empty otherwise |
| `source` | string | Worktree source such as `.codex`, `.somename`, `project-local`, or the registered `superpowers`; empty for non-worktree items. Superpowers rows use `tool=unknown`. |
| `path` | string | Absolute filesystem path |
| `size` | integer | Size in bytes |
| `mod_time` | string | Last modification time in RFC 3339 format. For `build-cache` and `other-cache` rows this is the newest mtime found anywhere in the cache tree, not the path's own mtime. |
| `status` | string | Worktree health (`active`, `orphaned`, `plain-dir`) or empty for non-worktree items. Only scanner-validated `active` and `orphaned` worktree rows can enter cleanup safety; `plain-dir`, empty, and unknown values are review-only. |
| `classification` | string | Agent-state health (`live`, `orphaned`, `undetermined`), omitted for items outside `agent-state`. Cursor project-store entries derive this from all distinct absolute `workspacePath=` values in `worker.log` that are outside `~/.cursor`; any live path wins and `orphaned` requires every usable path to be proven absent. |
| `risk` | string | Derived cleanup risk (`low`, `medium`, `high`) |
| `reason` | string | Short derived explanation for cleanup review |
| `cleanup_kind` | string | Cleanup strategy (`remove-path` or `command`) |
| `cleanup_command` | array | Argv command used when `cleanup_kind` is `command`; empty for path removal |

`risk` and `reason` are presentation fields derived from `category`, `status`,
and `classification`; they are intended for human and AI-assisted cleanup
decisions.

Worktree units support only a direct `.git` marker or markers in immediate
project children. A readable unit without valid metadata is emitted once as
`plain-dir` with an explicit `reason`. If valid and invalid immediate members
are mixed, that same one-row owner representation prevents the valid sibling
from becoming executable. An I/O failure while inspecting a container or
marker is not `plain-dir`; it is a top-level partial provider error.

For Cursor `agent-state`, `project` is the final path segment of the recorded
workspace, not a decoded form of the project-store directory name. Missing,
unreadable, or unusable `worker.log` evidence produces `undetermined`.
Orphaned Cursor entries are eligible for default cleanup without an age gate;
`live` and `undetermined` entries remain protected.

### `summary` object

| Field | Type | Description |
| ------- | ------ | ------------- |
| `total_count` | integer | Total number of debris items |
| `total_size` | integer | Total size in bytes |
| `by_category` | object | Per-category counts and sizes |
| `by_tool` | object | Per-tool counts and sizes |

### Partial-scan fields

| Field | Type | Description |
| ----- | ---- | ----------- |
| `partial` | boolean | Present and `true` only when at least one provider failed |
| `provider_errors` | array | Failed provider names and related error messages; present only for partial scans |

### `by_category` / `by_tool` entries

| Field | Type | Description |
| ----- | ---- | ----------- |
| `count` | integer | Number of items |
| `size` | integer | Total size in bytes |

## Retention projection (read-only, shipped)

The top-level `retention` object is always present. It is non-additive
physical accounting: one aggregate row exists per `(store_id, bucket_id)`,
aggregate values are never summed with `summary`, and each existing physical
owner remains counted once. Aggregate rows are never executable `DebrisInfo`
rows and never authorize cleanup.

| Field | Type | Description |
| --- | --- | --- |
| `buckets` | array | One aggregate row per `(store_id, bucket_id)`, including protected open and `unknown` buckets, ordered by store ID then bucket ID. |
| `partial` | boolean | `true` when a retention store could not be fully inventoried (permission or I/O failure inside the store, or provider misconfiguration). Retention partiality does not set the top-level `partial` flag. |
| `provider_errors` | array | Path-free store-local diagnostics; present regardless of partiality. |

### `retention.buckets[]` fields

| Field | Type | Description |
| --- | --- | --- |
| `store_id` | string | Exact registered retention store ID (`codex-sessions`). |
| `bucket_id` | string | UTC month `YYYY-MM`, or visible protected `unknown`. |
| `unit_count` | integer | Bounded retention units in the bucket. |
| `member_count` | integer | Owned physical regular-file leaves in those units. |
| `apparent_bytes` | integer | Deduplicated owned `Lstat.Size` bytes; not allocated or guaranteed freed bytes. |
| `orphaned_count` | integer | Codex orphan-statistics subset (proven-absent recorded cwd); zero for stores without that contract. |
| `orphaned_bytes` | integer | Apparent bytes in the Codex orphan subset; never added to `apparent_bytes`. |

No member path, session identifier, or transcript content appears in the
projection or its diagnostics. The execution-layer selector, manifest, and
`--retention-bucket` spelling remain parked (see
[PROTECTED_RETENTION.md](PROTECTED_RETENTION.md)).

## Clean dry-run plan

### Invocation and privacy

```bash
aibris clean --no-guide --dry-run --json
aibris clean --no-guide --dry-run --json --include-paths
aibris clean --no-guide --json --force
aibris clean --no-guide --json --interactive
```

The default clean JSON document is path-redacted. Its successful stdout is
exactly one JSON document and successful stderr is empty; it contains no home
directory, project label, raw path, cleanup argv, blocker/member/obligation
path, or internal canonical key. `--include-paths` opts in to explicit
`path`, `project`, and `cleanup_command` fields on logical rows and `path` on
physical targets. It never includes external command output.

`--include-paths` without `--json` fails. Non-dry-run `clean --json` requires
either `--force` or `--interactive`; execution always takes the classic route,
and an explicit `--guide` fails before scan or mutation. Use the
same classic selectors for preview and execution (for example,
`clean --no-guide --dry-run --json` and then
`clean --no-guide --json --force`), changing only `--dry-run`. JSON mode never
writes prompts or progress text to stdout. Dry-run guided cleanup uses the
existing deterministic defaults (recommended rows selected, reviewable rows
held, and locked rows protected). Non-dry-run JSON uses the same redaction
contract as the plan.

`--force --json` attempts the complete selected set without a confirmation
read. `--interactive --json` reads one silent line per selected physical target
in embedded `plan.physical_targets` order:
`y`/`yes` executes that target, `n`/`no` records it as non-requested
`skipped`, and invalid or missing input cancels that target and the remaining
requests. If deletion-time safety changes the selected physical-target set,
the receipt fails closed before consuming confirmation input. A confirmation is followed by the unified plan validation, and the
prepared executor repeats target identity, cached-evidence, Git, and
agent-state checks immediately before mutation.

### Shape

The clean-plan document has this top-level shape:

```json
{
  "schema_version": 1,
  "document_type": "clean_plan",
  "mode": "dry_run",
  "paths_included": false,
  "evidence": {
    "complete": true,
    "source": "live",
    "observed_at": "2026-08-08T12:00:00Z"
  },
  "policy": {
    "minimum_age": "7d",
    "categories": [],
    "tools": [],
    "risky": false,
    "include_active_worktrees": false
  },
  "totals": {
    "visible_rows": 1,
    "physical_targets": 1,
    "physical_bytes": 4096,
    "selected": 1,
    "selected_bytes": 4096,
    "reviewable": 0,
    "reviewable_bytes": 0,
    "protected": 0,
    "protected_bytes": 0,
    "skipped": 0,
    "skipped_bytes": 0
  },
  "physical_targets": [
    {
      "id": "target-1",
      "decision": "selected",
      "bytes": 4096,
      "category": "node_modules",
      "tool": "node_modules",
      "cleanup_kind": "remove-path"
    }
  ],
  "rows": [
    {
      "id": "row-1",
      "physical_target_id": "target-1",
      "relation": "owner",
      "policy_decision": "eligible",
      "decision": "selected",
      "category": "node_modules",
      "tool": "node_modules",
      "reason_codes": ["classic_eligible"]
    }
  ]
}
```

`physical_targets` is the byte-owning list. IDs are deterministic and
document-local (`target-1`, `target-2`, ...); they are not hashes or path
identifiers. Each target is one containment-normalized physical component, so
exact duplicate discoveries never add another target or another byte. A
reviewable parent and a selected/locked child remain separate action owners to
preserve cleanup safety; their `bytes` values are nevertheless containment-
disjoint. Descendant components receive their claimed size first in canonical
path order and each parent receives its remaining exclusive share. If children
consume the parent estimate, that parent deterministically has `bytes: 0` but
retains its decision and action identity. A descendant's claimed accounting is
also capped by the remaining budget of its containing owner, so nested size
estimates cannot make a containment tree exceed its outer owner budget.
`rows` is the visible logical evidence list. Rows have no size field and use
`owner`, `exact`, `nested`, or `ancestor` relations; every row references
exactly one `physical_target_id`. `ancestor` means the row's discovered path
contains the physical target owner (for example, a protected active worktree
parent containing a selected `node_modules` owner). All arrays, including empty
arrays, are emitted as `[]`.

`rows[].decision` mirrors the final decision of the referenced
`physical_target_id`; it is not a second logical-row action decision.
`policy_decision` classifies the logical row's source policy:
classic eligible rows use `eligible`, guided recommendations use
`recommended`, guided soft holds use `reviewable`, hard locks and safety
protections use `protected`, and filtered/noncandidate inventory uses
`skipped`. `reason_codes` contains stable machine-readable codes only; human
reason descriptions are not part of this contract. Only `physical_targets` are
action rows. An `ancestor` row is evidence attached to the selected physical
owner; its ancestor path is never an additional action target. A selected
target may therefore carry protected or reviewable evidence with a
`protected_overlap` reason code without becoming locked.

`policy.minimum_age` is always the classic filter age actually passed in
`opts.Age`, including on the mixed auto-guided route. When guided state exists,
`policy.guided_min_idle_age` is additionally emitted with the guided policy's
minimum idle age. It is omitted for classic-only plans. Thus the default
auto-guided route reports `minimum_age: "7d"` and
`guided_min_idle_age: "3d"`; an explicit guided route with its omitted age
uses `3d` for both values.

The byte accounting invariants are:

- bytes are owned by `physical_targets`; rows do not carry sizes;
- `physical_bytes` includes every emitted containment-disjoint physical target,
  including skipped and protected inventory;
- `selected_bytes` is the actionable subset of `physical_bytes`;
- `selected + reviewable + protected + skipped` equals `physical_targets`;
- the corresponding byte totals sum to `physical_bytes`;
- changing exact or nested logical row counts does not inflate physical counts
  or bytes.

The human cleanup review uses evaluated policy descriptions, not machine reason
codes. Guided rows keep their aggregate explanation once; reason entries with
empty descriptions are omitted from human text, while all stable codes remain
available in the JSON plan.

## Clean execution receipt

Non-dry-run JSON execution emits a single top-level receipt:

```json
{
  "schema_version": 1,
  "document_type": "clean_receipt",
  "mode": "execute",
  "paths_included": false,
  "status": "succeeded",
  "plan": { "document_type": "clean_plan", "mode": "dry_run" },
  "totals": {
    "requested": 1,
    "removed": 1,
    "partial": 0,
    "failed": 0,
    "cancelled": 0,
    "protected": 0,
    "reviewable": 0,
    "skipped": 0,
    "freed_bytes": 4096
  },
  "physical_targets": [
    {
      "id": "target-1",
      "decision": "selected",
      "state": "removed",
      "requested": true,
      "bytes": 4096,
      "freed_bytes": 4096,
      "physical_removed": true,
      "category": "node_modules",
      "tool": "node_modules",
      "cleanup_kind": "remove-path",
      "reason_codes": ["classic_eligible", "removed"]
    }
  ]
}
```

The embedded `plan` is the accepted plan built in the same process. Its
document-local `target-*` IDs are reused by receipt `physical_targets`; no
path-derived or externally supplied ID authorizes execution. Receipt target
rows are physical owners only. Logical rows remain inside the embedded plan
and never contribute bytes.

`status` is one of `succeeded`, `partial_failure`, `failed`, or `cancelled`.
Only `succeeded` exits zero. Receipt accounting is physical-target based:

- `requested = removed + partial + failed + cancelled`;
- protected, reviewable, and skipped targets are not requested;
- `freed_bytes` is credited only for a target whose physical owner was
  verified absent after its mutation; it is never inferred from logical rows;
- `partial_failure` means execution both made progress or left a partial
  mutation and encountered a partial, failed, or cancelled request;
- `failed` means at least one requested target failed without any successful
  or partial mutation; `cancelled` means requests were cancelled without a
  removal, partial mutation, or failure.

The default receipt is redacted exactly like the plan. With `--include-paths`,
physical target paths and the embedded plan's logical paths, projects, and
cleanup commands are included. Error and refusal reason codes remain stable
and path-free; external command output is never copied into JSON. A
`command_fallback_path_removal` reason code records that a missing planned
cleanup command reached its safe path-removal fallback.
