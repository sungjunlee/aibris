# aibris

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![CI](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml/badge.svg)](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sungjunlee/aibris)](https://goreportcard.com/report/github.com/sungjunlee/aibris)

AI + debris. A small CLI for cleaning up the filesystem leftovers from AI
coding sessions: worktrees, logs, `node_modules`, and build caches.

AI tools are productive, but they shed a lot of temporary state while they
branch, build, test, and retry. aibris scans the places that debris tends to
collect, shows a readable cleanup plan, and only deletes after filters,
confirmation, and path safety checks.

## Who is this for?

- Developers who use AI coding tools that create Git worktrees under `$HOME`
- Teams sharing development machines where worktrees accumulate
- Anyone who wants to reclaim disk space from node_modules and build caches
- AI assistants that need structured scan output before cleanup

## What it cleans

| Category | Examples | Default clean |
|----------|----------|---------------|
| AI worktrees | `$HOME` worktree conventions such as `.tool/worktrees` and project-local `worktrees` | Classic: orphaned; guided Codex: evidence-based |
| Dependencies | project `node_modules` directories | Yes |
| Build caches | Go, npm, Gradle, Cargo, Xcode | Yes |
| Python caches | pip and uv cache directories | Yes |
| AI logs | Codex, Claude, Cursor, Windsurf logs | Only with `--risky` |

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
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- 0.7.0
```

The installer downloads GitHub Release binaries and verifies `checksums.txt`.
The default install path uses GitHub's `releases/latest/download` URLs for
prebuilt binaries. `main` builds from source with Go.

By default, aibris installs to `~/.local/bin` and does not require `sudo`. If
that directory is not on your `PATH`, the installer prints the exact command to
add it for your shell. For a system-wide install, pass an explicit prefix:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- --prefix /usr/local/bin
```

### Usage

```bash
aibris scan                    # discover what's taking space
aibris scan --json             # machine-readable output (see docs/JSON_SCHEMA.md)
aibris scan --root ~/.codex    # narrow scan to a home subdirectory

aibris clean --dry-run         # preview without deleting
aibris clean --no-guide --dry-run # force classic cleanup audit
aibris clean                   # delete with confirmation
aibris clean --root ~/.codex --dry-run
aibris clean --age 7d          # classic filter, or guided minimum idle age
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
  found size  3.2 GB
  default clean 3.1 GB
  protected   96.0 MB active worktrees; use --include-active-worktrees after review

by category
  node_modules    1   1.8 GB
  build-cache     2   1.3 GB
  worktree        1   96.0 MB

largest
    1.8 GB  node_modules  dashboard    -                  24d
  842.0 MB  build-cache   go-build     global             9d
  512.4 MB  build-cache   npm          global             18d
   96.0 MB  worktree      b7f4c2       aibris             active today

next
  aibris clean --dry-run
  aibris scan --json
```

Preview before deleting anything:

```text
$ aibris clean --category worktree --age 7d --dry-run
clean
  roots  ~

  policy  age>7d, risky=false, active-worktrees=protected
  scan    cached, 8s old

scan summary
  scanned    7 sources   3 items   2.0 GB
  eligible   1 item   96.0 MB
  protected/skipped 2 items   1.9 GB

by category
  category             found     eligible  protected/skipped  main reason
  worktree          2  192.0 MB   1  96.0 MB   1  96.0 MB  active worktree protected
  node_modules      1    1.8 GB   0      0 B   1   1.8 GB  outside category/tool filters

  matched  1 candidate   96.0 MB

clean plan
  mode     dry-run
  targets  1 item   96.0 MB

targets
      size  category      name         project            age/status     action       reason
   96.0 MB  worktree      b7f4c2       aibris             orphaned 12d   remove-path  orphaned worktree; parent repo metadata missing
    ~/.codex/worktrees/b7f4c2

[DRY-RUN] No files were removed.
```

When active Codex worktrees are the useful cleanup decision and no classic
cleanup selector is supplied, `aibris clean --dry-run` opens guided Codex
worktree review by default. This includes protected-only pressure: at least one
validated active Codex cleanup unit and either 256 MB total or three units. The
guide defaults recommended rows to selected, keeps reviewable and locked rows
visible, lets you toggle selectable rows by number, and still hands the final
selection to the normal dry-run plan before anything can be deleted:

```bash
aibris clean --dry-run
```

The guided policy operates on physical cleanup units. A unit is sized and
removed once, but every direct or one-level nested Git worktree member must pass
safety inspection. Members are grouped for retention by canonical Git
common-dir, not by the path-derived project label.

Policy evaluation is ordered:

1. Lock the unit when it contains the current directory, dirty or untracked
   files, unreadable Git or Codex activity evidence, a detached HEAD unreachable
   from named refs, or activity within the last 6 hours.
2. Keep the three most recently active units per canonical repository as
   reviewable and unselected. A user may explicitly select these soft holds.
3. Keep units younger than the guided minimum idle age (3 days by default) or
   smaller than 256 MB reviewable and unselected.
4. Recommend and select the remaining units.

An attached local branch is recoverable even without an upstream. A detached
HEAD is recoverable when a local or remote named ref contains it. Missing or
gone upstream is shown as explanatory metadata and never locks a row by itself.
Changing `--age` or using the prompt's `age`, `+`, `-`, `[` or `]` commands
changes only the minimum idle age; the 6-hour lock and recent-three ranking do
not change.

The guide reads only Codex session metadata such as timestamps and working
directories, never conversation bodies. A real deletion still requires the
dry-run preview first and then the normal confirmation prompt unless `--force`
is explicitly provided. `--force` skips only that prompt: it cannot select a
locked row and is never passed to `git worktree remove`. Use `--no-guide` to
keep the classic cleanup audit/executor route, or `--guide` to force guided
Codex review.

Confirm before deleting anything:

```text
$ aibris clean --category node_modules --age 7d
clean
  roots  ~

  policy  age>7d, risky=false, active-worktrees=protected
  scan    cached, 11s old

scan summary
  scanned    7 sources   4 items   3.2 GB
  eligible   1 item   1.8 GB
  protected/skipped 3 items   1.4 GB

by category
  category             found     eligible  protected/skipped  main reason
  node_modules      1    1.8 GB   1   1.8 GB   0      0 B  eligible for cleanup
  build-cache       2    1.3 GB   0      0 B   2   1.3 GB  outside category/tool filters
  worktree          1   96.0 MB   0      0 B   1  96.0 MB  active worktree protected

  matched  1 candidate   1.8 GB

clean plan
  mode     delete
  targets  1 item   1.8 GB

targets
      size  category      name         project            age/status     action       reason
    1.8 GB  node_modules  dashboard    -                  24d           remove-path  dependency directory; can be reinstalled
    ~/path/to/dashboard/node_modules

Proceed? [y/N]: y
removing 1/1: dashboard (node_modules) ...
removed: dashboard (node_modules) — 1.8 GB

cleanup receipt
  targets    1 item
  freed      1.8 GB
  protected/skipped 3 items   1.4 GB
```

`scan` writes a short-lived snapshot under the user cache directory. A following
`clean` reuses it for 5 minutes when the scan roots and cache schema match. If
the cache is stale, missing, or for different roots, `clean` falls back to a
live scan with progress output.

Live fallback keeps the same audit shape after non-interactive scan progress:

```text
clean
  roots  ~

  scanning node_modules
  scanning build-cache
  found    build-cache    2 items   1.3 GB
  found    node_modules   1 items   1.8 GB

  policy  age>7d, risky=false, active-worktrees=protected
  scan    live

scan summary
  scanned    7 sources   3 items   3.1 GB
  eligible   1 item   1.8 GB
  protected/skipped 2 items   1.3 GB
```

For unscoped guided Codex cleanup, the no-selector loop is fast and visible:

```bash
aibris scan
aibris clean --dry-run
aibris clean
```

This plain-command pair is not a substitute for a scoped cleanup. If the user
approves selectors or safety flags, keep every flag and value identical in the
preview and execution commands and remove only `--dry-run` for execution.

When stdout is an interactive terminal, scans use a single-line spinner while
providers run. In non-interactive logs, progress falls back to plain
`scanning` / `found` lines.

If a provider fails but other providers return usable results, `scan` labels
the inventory as partial, lists the failed providers, emits the retained human
or JSON result, and exits with status 1. Partial scans are never cached for
cleanup, and `clean` requires a complete scan before it can plan or remove
anything. A partial scan also invalidates any previous cleanup scan cache.
Cancellation remains a hard failure.

### Safety

- **Independent age policies**: classic cleanup defaults to `--age 7d`; guided
  Codex cleanup defaults to a 3-day minimum idle age while always keeping its
  6-hour recent-activity lock and recent-three retention
- **Human age units** support `h`, `d`, `w`, `mo`, and `y`
- **`--dry-run`** previews before deleting
- **`--interactive`** confirms each item
- **Target plan before final confirmation** shows category, size, project,
  age/status, path, and cleanup command when applicable
- **Guided Codex cleanup** classifies physical units as recommended,
  reviewable, or locked after member-level Git and activity checks, then uses
  the same dry-run and confirmation model as regular `clean`
- **Git-aware active removal** preflights every member, removes it with
  `git worktree remove` semantics, preserves attached branch refs and referenced
  detached commits, and verifies parent worktree metadata. It never falls back
  to recursive deletion after Git removal fails.
- **Recent scan reuse** skips a repeated scan when `clean` can use a fresh
  compatible snapshot, while still re-checking target paths
- **`--risky`** must be explicitly set to delete AI logs
- **Active worktrees are excluded by default**; use
  `--include-active-worktrees` only when you intentionally want age-based
  cleanup for valid worktrees
- **Home-scoped roots**: default scanning starts at `$HOME`; `--root` can narrow
  scope to one or more existing directories under `$HOME`
- **Convention-based worktree discovery**: worktrees are discovered by finding
  `worktrees`, `worktree`, `worktree-*`, and `worktrees-*` directories under
  scan roots, then validating direct or nested `.git` files. To keep full-home
  scans practical, aibris searches hidden owners and project-local containers
  within a bounded shallow depth instead of recursively walking every child.
- **Pruned scan directories** for project-style discovery include `.Trash`,
  `Library`, `Applications`, `Pictures`, `Movies`, `Music`, `.git`, `vendor`,
  and nested `node_modules`; `Desktop` and `Downloads` are scanned
- **Official cache cleanup commands** are preferred for supported caches
  (`go clean -cache`, `npm cache clean --force`, `uv cache prune`). If the
  owning command is missing, aibris falls back to the existing safe path removal
  behavior; if the command runs and fails, aibris does not fall back silently.
- **Confirmation prompt** on every `clean` (use `--force` to skip only the
  prompt; hard locks and non-forced Git removal remain unchanged)
- **Safety validation** rejects deletions outside `$HOME`, symlink escapes, and
  unvalidated arbitrary paths. Generic worktrees are only cleanable after scan
  metadata proves they are active or orphaned Git worktrees.
- **Negative age rejection** prevents accidental full-scope deletion

### How It Works

```
aibris scan  → discovers worktree conventions, caches, node_modules, logs under scan roots
aibris clean → filters or plans evidence-based units → previews → deletes safely
```

AI tools leave debris in predictable conventions. aibris scans `$HOME` by
default, prunes high-noise system and media directories while discovering
development debris, validates Git worktree metadata before reporting worktrees,
measures disk usage, and cleans only after filters and safety checks.
Judgment about what should be removed stays with a human or an AI assistant
using `scan --json`.

New tools can be added by implementing the `DebrisProvider` interface.

### Agent Workflow

No-selector guided Codex cleanup:

```bash
aibris scan --json
aibris clean --dry-run
aibris clean
```

Scoped cleanup keeps every approved selector and safety flag identical between
preview and execution; only `--dry-run` is removed:

```bash
aibris scan --json
aibris clean --no-guide --category worktree --age 7d --dry-run
aibris clean --no-guide --category worktree --age 7d
```

The intended agent flow is: scan, summarize by project/category/age, use guided
review for active Codex pressure, run a dry-run, ask again, then execute. Treat
`active` as linked Git metadata, not proof of recent use; rely on the guided
class and reason before proposing an active unit. A scoped execution must never
fall back to plain `aibris clean`: preserve all approved `--category`, `--tool`,
`--root`, `--age`, routing, and safety flags.

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for architecture and development guidelines.

### License

MIT — see [LICENSE](LICENSE).
