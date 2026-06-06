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
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash
```

Install from the current main branch when you want unreleased changes:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- main
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- 0.3.2
```

The installer downloads GitHub Release binaries and verifies `checksums.txt`.
The default install path uses GitHub's `releases/latest/download` URLs for
prebuilt binaries. `main` builds from source with Go.

### Usage

```bash
aibris scan                    # discover what's taking space
aibris scan --json             # machine-readable output (see docs/JSON_SCHEMA.md)
aibris scan --root ~/workspace # limit scan to a home subdirectory

aibris clean --dry-run         # preview without deleting
aibris clean                   # delete with confirmation
aibris clean --root ~/workspace --dry-run
aibris clean --age 7d          # older than 7 days (default)
aibris clean --age 30d         # older than 30 days
aibris clean --age 1mo         # older than 30 days (month shorthand)
aibris clean --age 1y          # older than 365 days
aibris clean --interactive     # confirm each item
aibris clean --category node_modules   # only node_modules
aibris clean --tool codex,claude       # only specific tools
aibris clean --risky           # include ai-logs
aibris clean --include-active-worktrees # include active worktrees
aibris clean --force           # skip confirmation prompt
```

See [docs/DOGFOOD.md](docs/DOGFOOD.md) for real local scan transcripts used to
validate release behavior.

### Example

```text
$ aibris scan

scan
  roots  ~

  scanned  7 sources   4 items   3.2 GB

summary
  found       4 items
  reclaimable 3.2 GB

by category
  node_modules    1   1.8 GB
  build-cache     2   1.3 GB
  worktree        1   96.0 MB

largest
    1.8 GB  node_modules  dashboard    ?                  24d
  842.0 MB  build-cache   go-build     ?                  9d
  512.4 MB  build-cache   npm          ?                  18d
   96.0 MB  worktree      b7f4c2       aibris             active today

next
  aibris clean --dry-run
  aibris scan --json
```

Preview before deleting anything:

```text
$ aibris clean --category worktree --age 7d --dry-run
[DRY-RUN] would remove: b7f4c2 (codex) — 96.0 MB (12d ago)

[DRY-RUN] Total: 1 items | 96.0 MB would be freed
```

Confirm before deleting anything:

```text
$ aibris clean --category node_modules --age 7d
About to delete 1 items (1.8 GB).

targets
    1.8 GB  node_modules  dashboard    ?                  24d
    ~/workspace/dashboard/node_modules

Proceed? [y/N]:
```

When stdout is an interactive terminal, scan progress uses a single-line
spinner while providers run. In non-interactive logs, progress falls back to
plain `scanning` / `found` lines.

### Safety

- **Default `--age 7d`** avoids very recent work
- **Human age units** support `h`, `d`, `w`, `mo`, and `y`
- **`--dry-run`** previews before deleting
- **`--interactive`** confirms each item
- **Target plan before final confirmation** shows category, size, project,
  age/status, path, and cleanup command when applicable
- **`--risky`** must be explicitly set to delete AI logs
- **Active worktrees are excluded by default**; use
  `--include-active-worktrees` only when you intentionally want age-based
  cleanup for valid worktrees
- **Home-scoped roots**: default scanning starts at `$HOME`; `--root` can narrow
  scope to one or more existing directories under `$HOME`
- **Pruned scan directories** for project-style walks include `.Trash`,
  `Library`, `Applications`, `Pictures`, `Movies`, `Music`, `.git`, `vendor`,
  and nested `node_modules`; `Desktop` and `Downloads` are scanned
- **Official cache cleanup commands** are preferred for supported caches
  (`go clean -cache`, `npm cache clean --force`, `uv cache prune`). If the
  owning command is missing, aibris falls back to the existing safe path removal
  behavior; if the command runs and fails, aibris does not fall back silently.
- **Confirmation prompt** on every `clean` (use `--force` to skip)
- **`isSafePath` validation** rejects deletions outside known-safe directories
- **Negative age rejection** prevents accidental full-scope deletion

### How It Works

```
aibris scan  → discovers worktrees, caches, node_modules, logs under scan roots
aibris clean → filters by age/category/tool → deletes safely
```

AI tools leave debris in predictable locations. aibris scans `$HOME` by default,
prunes high-noise system and media directories while walking project-style
debris, measures disk usage, and cleans only after filters and safety checks.
Judgment about what should be removed stays with a human or an AI assistant
using `scan --json`.

New tools can be added by implementing the `DebrisProvider` interface.

### Agent Workflow

```bash
aibris scan --json
aibris scan --root ~/workspace --json
aibris clean --category worktree --tool codex --age 7d --dry-run
aibris clean --category worktree --tool codex --age 7d
```

The intended agent flow is: scan, summarize by project/category/age, ask the
user what to remove, run a dry-run, ask again, then execute.

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for architecture and development guidelines.

### License

MIT — see [LICENSE](LICENSE).
