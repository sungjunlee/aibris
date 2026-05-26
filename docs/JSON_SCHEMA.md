# aibris JSON Output Schema

`aibris scan --json` outputs the following JSON structure:

## Top-level structure

```json
{
  "worktrees": [
    {
      "tool": "codex",
      "category": "worktree",
      "id": "abc123",
      "project": "my-project",
      "path": "/Users/user/.codex/worktrees/abc123",
      "size": 102400,
      "mod_time": "2026-05-25T12:00:00Z"
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
| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name (`codex`, `claude`, `cursor`, `windsurf`, `node_modules`, `build-cache`, `pip-cache`, `ai-logs`) |
| `category` | string | Debris category (`worktree`, `node_modules`, `build-cache`, `other-cache`, `ai-logs`) |
| `id` | string | Unique identifier (hash, directory name, or cache key) |
| `project` | string | Project name if detectable, empty otherwise |
| `path` | string | Absolute filesystem path |
| `size` | integer | Size in bytes |
| `mod_time` | string | Last modification time in RFC 3339 format |

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
