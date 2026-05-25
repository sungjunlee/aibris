# aibris (AI + debris)

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Scan and clean up disk debris left behind by AI coding tools — worktrees, caches,
`node_modules`, and log files that silently consume gigabytes over time.

Supports: **Codex CLI**, **Claude Code**, **Cursor**, **Windsurf** (worktrees + logs),
plus **node_modules**, **Go/Gradle/npm/Cargo build caches**, and **pip/uv caches**.

### Install

```bash
brew install sungjunlee/tap/aibris
# or
go install github.com/sungjunlee/aibris@latest
```

### Usage

```bash
aibris scan                    # discover what's taking space
aibris scan --json             # machine-readable output

aibris clean --dry-run         # preview without deleting
aibris clean                   # requires confirmation (or --force)
aibris clean --age 168h        # older than 7 days (default)
aibris clean --age 720h        # older than 30 days
aibris clean --interactive     # confirm each item
aibris clean --category node_modules   # only node_modules
aibris clean --tool codex,claude       # only specific tools
aibris clean --risky           # include ai-logs
aibris clean --force           # skip confirmation prompt
```

### Safety

- **Default `--age 168h` (7 days)** protects active worktrees
- **`--dry-run`** previews before deleting
- **`--interactive`** confirms each item
- **`--risky`** must be explicitly set to delete AI logs
- **Confirmation prompt** on every `clean` (use `--force` to skip)

### How It Works

```
aibris scan  → discovers worktrees, caches, node_modules, logs
aibris clean → filters by age/category/tool → deletes safely
```

Each AI tool leaves its debris in predictable locations. aibris scans these
locations, measures disk usage, and cleans up what's no longer needed.
New tools can be added by implementing the `WorktreeProvider` interface.

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for architecture and development guidelines.

### License

MIT — see [LICENSE](LICENSE).
