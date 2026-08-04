# aibris JSON Output Schema

`aibris scan --json` outputs the following JSON structure. The output is
versioned; the top-level `schema_version` tells consumers which contract to
branch on.

## Versioning

- `schema_version` is `1` today. Consumers must treat an unknown (newer)
  `schema_version` as unsupported and stop rather than assume the shape.
- The canonical all-debris array is `items`; it represents every debris
  category.
- The historical field name `worktrees` is retained as a **0.x compatibility
  alias** and mirrors `items` exactly. It exists so existing 0.x consumers do
  not break, and is scheduled for removal after the 0.x compatibility period.
  New consumers should read `items`.

The installed/regenerable/protected terms used by the issue #142 planning
taxonomy are not JSON fields or values. They do not extend `category` or
`classification`, and the six stores covered by that planning decision
currently emit no inventory rows. This document describes only the shipped
schema.

The future protected-content retention surface is frozen in
[PROTECTED_RETENTION.md](PROTECTED_RETENTION.md), but it is not present in
current output. When implemented, it must be an additive top-level projection,
never aggregate rows disguised inside the historical `worktrees` array.

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
| `mod_time` | string | Last modification time in RFC 3339 format |
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
|-------|------|-------------|
| `partial` | boolean | Present and `true` only when at least one provider failed |
| `provider_errors` | array | Failed provider names and related error messages; present only for partial scans |

### `by_category` / `by_tool` entries

| Field | Type | Description |
|-------|------|-------------|
| `count` | integer | Number of items |
| `size` | integer | Total size in bytes |

## Future retention projection (contract only; unshipped)

No released `scan --json` output currently contains retention rows or the
fields below. A future schema revision may add a top-level retention projection
without changing the meaning of `worktrees` or existing `summary` values. That
projection is non-additive physical accounting: one aggregate row exists per
`(store_id, bucket_id)`, aggregate and manifest-member values are never summed,
and each existing physical owner remains counted once.

The minimum future aggregate fields are:

| Field | Type | Description |
| --- | --- | --- |
| `store_id` | string | Exact registered retention store ID. |
| `bucket_id` | string | UTC month `YYYY-MM`, or visible protected `unknown`. |
| `unit_count` | integer | Bounded retention units in the bucket. |
| `member_count` | integer | Owned physical regular-file leaves in those units. |
| `apparent_bytes` | integer | Deduplicated owned `Lstat.Size` bytes; not allocated or guaranteed freed bytes. |
| `orphaned_count` | integer | Codex orphan-statistics subset; zero for stores without that contract. |
| `orphaned_bytes` | integer | Apparent bytes in the Codex orphan subset; never added to `apparent_bytes`. |
| `selectable` | boolean | Whether the exact closed bucket may enter future manifest preparation. |
| `blocked_reason` | string | Fail-closed reason when the bucket cannot be selected. |

These aggregate rows will never be executable `DebrisInfo` rows. Only the
future exact-member manifest and revalidation path defined by the canonical
contract can prepare an explicitly selected closed bucket. The planned
`--retention-bucket` spelling is likewise unshipped.
