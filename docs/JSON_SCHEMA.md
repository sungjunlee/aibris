# aibris JSON Output Schema

`aibris scan --json` outputs the following JSON structure.

Compatibility note: the top-level array is named `worktrees` for historical
reasons, but it contains all debris items, including caches and `node_modules`.

## Top-level structure

```json
{
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

## Fields

### `worktrees` array

This array contains debris items from every category. Consumers should treat it
as an item list, not as a worktree-only list.

| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name (`codex`, `claude`, `unknown`, `cursor`, `windsurf`, `node_modules`, `build-cache`, `pip-cache`, `ai-logs`). Generic worktree owners may remain `unknown`. |
| `category` | string | Debris category (`worktree`, `node_modules`, `build-cache`, `other-cache`, `ai-logs`) |
| `id` | string | Unique identifier (hash, directory name, or cache key) |
| `project` | string | Project name if detectable, empty otherwise |
| `source` | string | Path-derived worktree source such as `.codex`, `.somename`, or `project-local`; empty for non-worktree items |
| `path` | string | Absolute filesystem path |
| `size` | integer | Size in bytes |
| `mod_time` | string | Last modification time in RFC 3339 format |
| `status` | string | Worktree health (`active`, `orphaned`, `plain-dir`) or empty for non-worktree items |
| `risk` | string | Derived cleanup risk (`low`, `medium`, `high`) |
| `reason` | string | Short derived explanation for cleanup review |
| `cleanup_kind` | string | Cleanup strategy (`remove-path` or `command`) |
| `cleanup_command` | array | Argv command used when `cleanup_kind` is `command`; empty for path removal |

`risk` and `reason` are presentation fields derived from `category` and
`status`; they are intended for human and AI-assisted cleanup decisions.

### `summary` object
| Field | Type | Description |
|-------|------|-------------|
| `total_count` | integer | Total number of debris items |
| `total_size` | integer | Total size in bytes |
| `by_category` | object | Per-category counts and sizes |
| `by_tool` | object | Per-tool counts and sizes |

### `by_category` / `by_tool` entries
| Field | Type | Description |
|-------|------|-------------|
| `count` | integer | Number of items |
| `size` | integer | Total size in bytes |
