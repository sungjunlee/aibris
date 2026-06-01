# aibris (AI + debris)

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![CI](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml/badge.svg)](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sungjunlee/aibris)](https://goreportcard.com/report/github.com/sungjunlee/aibris)

Clean AI coding workflow debris from known paths: worktrees, logs,
`node_modules`, and build caches.

AI tools create lots of short-lived filesystem state while they branch, build,
test, and retry. aibris scans the known places that debris tends to collect,
then lets you preview and clean it with explicit filters and safety checks.

## Who is this for?

- Developers who use AI coding tools (Codex CLI, Claude Code, Cursor, Windsurf)
- Teams sharing development machines where worktrees accumulate
- Anyone who wants to reclaim disk space from node_modules and build caches
- AI assistants that need structured scan output before cleanup

### Install

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/main/install.sh | bash
```

Install from the current main branch, useful before the first tagged release:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/main/install.sh | bash -s -- main
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/main/install.sh | bash -s -- 0.3.0
```

The installer downloads GitHub Release binaries and verifies `checksums.txt`.
If no release exists yet, it falls back to building `main` with Go.
`main`/`latest` always builds from source with Go.

### Usage

```bash
aibris scan                    # discover what's taking space
aibris scan --json             # machine-readable output (see docs/JSON_SCHEMA.md)

aibris clean --dry-run         # preview without deleting
aibris clean                   # delete with confirmation
aibris clean --age 7d          # older than 7 days (default)
aibris clean --age 30d         # older than 30 days
aibris clean --age 1mo         # older than 30 days (month shorthand)
aibris clean --age 1y          # older than 365 days
aibris clean --interactive     # confirm each item
aibris clean --category node_modules   # only node_modules
aibris clean --tool codex,claude       # only specific tools
aibris clean --risky           # include ai-logs
aibris clean --force           # skip confirmation prompt
```

### Example

```text
$ aibris scan

node_modules:
  → dashboard       ?                      1.8 GB  24d ago

build-cache:
  → go-build        ?                    842.0 MB  9d ago
  → npm             ?                    512.4 MB  18d ago

codex:
  → b7f4c2          aibris                96.0 MB  12d ago

Total: 4 items | 3.2 GB
```

Preview before deleting anything:

```text
$ aibris clean --category worktree --age 7d --dry-run
[DRY-RUN] would remove: b7f4c2 (codex) — 96.0 MB (12d ago)

[DRY-RUN] Total: 1 items | 96.0 MB would be freed
```

### Safety

- **Default `--age 7d`** avoids very recent work
- **Human age units** support `h`, `d`, `w`, `mo`, and `y`
- **`--dry-run`** previews before deleting
- **`--interactive`** confirms each item
- **`--risky`** must be explicitly set to delete AI logs
- **Confirmation prompt** on every `clean` (use `--force` to skip)
- **`isSafePath` validation** rejects deletions outside known-safe directories
- **Negative age rejection** prevents accidental full-scope deletion

### How It Works

```
aibris scan  → discovers worktrees, caches, node_modules, logs
aibris clean → filters by age/category/tool → deletes safely
```

AI tools leave debris in predictable locations. aibris scans those locations,
measures disk usage, and cleans only after filters and safety checks. Judgment
about what should be removed stays with a human or an AI assistant using
`scan --json`.

New tools can be added by implementing the `DebrisProvider` interface.

### Agent Workflow

```bash
aibris scan --json
aibris clean --category worktree --tool codex --age 7d --dry-run
aibris clean --category worktree --tool codex --age 7d
```

The intended agent flow is: scan, summarize by project/category/age, ask the
user what to remove, run a dry-run, ask again, then execute.

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for architecture and development guidelines.

### License

MIT — see [LICENSE](LICENSE).
