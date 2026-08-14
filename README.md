# aibris

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![CI](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml/badge.svg)](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sungjunlee/aibris)](https://goreportcard.com/report/github.com/sungjunlee/aibris)

AI + debris. A small CLI for cleaning up the filesystem leftovers from AI
coding agents (Codex CLI, Claude Code, Cursor, Windsurf): Git worktrees,
agent session stores, AI logs, and recorded-cwd agent state — with generic
build debris (`node_modules`, build caches, pip/uv caches) as complementary
coverage so `scan` stays a complete picture of one home.

AI tools are productive, but they shed a lot of temporary state while they
branch, build, test, and retry. aibris scans `$HOME` for the places that
debris collects, shows you how much space is found, how much is reclaimable,
and what is protected — then deletes only after filters, a preview, and
confirmation.

## Platforms

- **macOS and Linux**: first-class. The `install.sh` installer downloads
  verified release binaries; no `sudo` needed by default.
- **Windows**: experimental archives. See the canonical
  [Windows support contract](docs/WINDOWS.md) for native installation, tested
  behavior, and unaudited boundaries. `install.sh` remains Unix/Bash-only.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash
```

Install from the current main branch when you want unreleased changes:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- main
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- 0.8.1
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

## Quick start: scan → dry-run → clean

The core loop is three commands:

```bash
aibris scan             # 1. discover what's taking space
aibris clean --dry-run  # 2. preview a cleanup plan without deleting
aibris clean            # 3. review the plan and confirm before deletion
```

### 1. Scan

`aibris scan` inventories debris under `$HOME` (or under `--root` subpaths)
and reports found space, a default-clean estimate of reclaimable space, and
what is held back by age, `--risky`, or protection:

### Exclusions

`scan` and `clean` accept repeatable `--exclude` paths or glob patterns to
hide private, slow, or intentionally retained trees from discovery:

```bash
aibris scan --exclude ~/work/secret-project
aibris clean --exclude ~/worktrees/keep-me --dry-run
```

An exclusion pattern is only honored when it resolves inside the approved
scan roots; patterns that escape the roots (absolute paths elsewhere, `..`
traversal, or symlinks pointing outside) are rejected and reported.
Exclusions affect discovery only: they remove paths from scan results and can
never make a path cleanable.

Persistent exclusions live in `$XDG_CONFIG_HOME/aibris/ignore` (falling back
to `~/.config/aibris/ignore`), one pattern per line with `#` comments. A
repo-local `.aibris-ignore` file directly under a scan root works the same
way. Flag and ignore-file patterns are merged; without any of them, defaults
are unchanged.

### Example

```text
$ aibris scan --root ~/aibris_demo

scan
  roots  ~/aibris_demo

  scanning node_modules
  scanning build-cache 
  scanning pip-cache   
  scanning cursor      
  scanning claude      
  scanning ai-logs     
  scanning windsurf    
  scanning codex       
  found    pip-cache      0 items   0 B

  found    cursor         0 items   0 B

  found    claude         0 items   0 B

  found    windsurf       0 items   0 B

  found    ai-logs        1 items   94.2 MB

  found    codex          0 items   0 B

  found    build-cache    0 items   0 B

  found    node_modules   2 items   20.0 KB

summary
  found       3 items
  found size  94.2 MB
  default clean (estimate) 0 B
  age-blocked 20.0 KB younger than 7d
  risky       94.2 MB requires --risky

by category
  ai-logs         1   94.2 MB
  node_modules    2   20.0 KB

largest
   94.2 MB  ai-logs       codex-logs   global             today
   12.0 KB  node_modules  projA        -                  today
    8.0 KB  node_modules  projB        -                  today

retention (protected content, read-only)
  codex-sessions   2026-08  units 55  members 55  4.5 MB  orphaned 0/0 B

next
  aibris clean --dry-run
  aibris scan --json
```

Reading the summary:

- **found** — everything on disk in scope (94.2 MB here).
- **default clean (estimate)** — what a default `aibris clean` would reclaim.
  It is an estimate; run `aibris clean --dry-run` for the exact plan.
- **age-blocked** and **risky** — space held back by the default `7d` age
  filter or by the explicit `--risky` gate for AI logs.
- **retention** — a read-only inventory of protected Codex session content;
  it never becomes a cleanup candidate. See
  [docs/PROTECTED_RETENTION.md](docs/PROTECTED_RETENTION.md).

### 2. Preview (dry-run)

`aibris clean --dry-run` plans the deletion without touching anything. This
example widens the age filter and scopes to `node_modules`:

```text
$ aibris clean --no-guide --dry-run --age 1s --category node_modules --root ~/aibris_demo

clean
  roots  ~/aibris_demo

  policy  age>1s, risky=false, active-worktrees=protected
  scan    cached, 15s old

scan summary
  scanned    8 sources   3 physical items   94.2 MB   3 evidence rows
  eligible   2 items   20.0 KB
  protected/skipped 1 item   94.2 MB

by category
  category             found     eligible  protected/skipped evidence  main reason
  ai-logs         1  94.2 MB   0      0 B         1  94.2 MB        1  outside category/tool filters
  node_modules    2  20.0 KB   2  20.0 KB         0      0 B        2  eligible for cleanup

  matched  2 candidates   20.0 KB

clean plan
  mode     dry-run
  targets  2 items   20.0 KB

targets
      size  category      name         project            age/status     action       reason
   12.0 KB  node_modules  projA        -                  today          remove-path  dependency directory; can be reinstalled
    ~/aibris_demo/projA/node_modules
    8.0 KB  node_modules  projB        -                  today          remove-path  dependency directory; can be reinstalled
    ~/aibris_demo/projB/node_modules

[DRY-RUN] No files were removed.
```

The plan separates **eligible** (reclaimable) targets from
**protected/skipped** space, shows the exact paths, and only executes after
you drop `--dry-run` and confirm.

### 3. Clean

Run the same command without `--dry-run` to execute. aibris prints the plan,
asks `Proceed? [y/N]:`, and only then deletes:

```bash
aibris clean --no-guide --age 1s --category node_modules --root ~/aibris_demo
```

Automation can drive the same loop with machine-readable output — see
[JSON output](docs/JSON_SCHEMA.md):

```bash
aibris scan --json                        # machine-readable inventory
aibris clean --no-guide --dry-run --json  # machine-readable cleanup plan
```

## What it cleans

| Category | Examples | Default clean |
| ---------- | ---------- | --------------- |
| AI worktrees | Finite known containers plus `$HOME` conventions such as `.tool/worktrees` and project-local `worktrees` | Classic: orphaned; guided Codex: evidence-based |
| Agent state | Claude and Cursor project stores | Orphaned only by proof; default selection waits for `--agent-state-grace` (24h) |
| AI logs | Codex, Claude, Windsurf logs | Only with `--risky` |
| Dependencies | project `node_modules` directories | Yes |
| Build caches | Go, npm, Gradle, Cargo, Xcode | Yes |
| Python caches | pip and uv cache directories | Yes |

The first three rows are **agent-produced state** — aibris's subject. The last
three are **generic build debris**: aibris covers them so `scan` reports a
complete picture of a home, but general-purpose cleaners already handle them
and winning on them is not an objective. Category-level definitions and future
store constraints live in [docs/CATEGORY.md](docs/CATEGORY.md).

## Common commands

```bash
aibris scan                    # discover what's taking space
aibris scan --json             # machine-readable output (see docs/JSON_SCHEMA.md)
aibris scan --root ~/.codex    # narrow scan to a home subdirectory

aibris clean                   # guided or classic cleanup with confirmation
aibris clean --dry-run         # preview without deleting
aibris clean --root ~/.codex --dry-run
aibris clean --age 7d          # classic filter, or guided minimum idle age
aibris clean --age 30d         # older than 30 days
aibris clean --age 1mo         # month shorthand
aibris clean --age 1y          # older than 365 days
aibris clean --category node_modules   # only node_modules
aibris clean --tool codex,claude       # only specific tools
aibris clean --risky           # include ai-logs
aibris clean --interactive     # confirm each item
aibris clean --include-active-worktrees # include active worktrees
aibris clean --agent-state-grace 0      # drop the orphaned agent-state idle floor (default 24h)
aibris clean --no-guide        # force the classic cleanup audit
aibris clean --guide           # force guided Codex worktree review
aibris clean --force           # skip the confirmation prompt only
aibris clean --guide --force --receipt-file cleanup.json  # machine-readable execution receipt
```

All flags come from `aibris --help`, `aibris scan --help`, and
`aibris clean --help`. See [docs/DOGFOOD.md](docs/DOGFOOD.md) for real local
scan transcripts used to validate release behavior.

## Safety

- **Preview first**: `--dry-run` plans without deleting; every real `clean`
  asks for confirmation (`--force` skips only the prompt, never a safety lock)
- **`--interactive`** confirms each item individually
- **Default age floor**: classic cleanup defaults to `--age 7d` (units `h`,
  `d`, `w`, `mo`, `y`, plus Go duration units such as `s`); negative ages are rejected
- **`--risky` required** to touch AI logs
- **Active worktrees excluded by default**; opt in with
  `--include-active-worktrees` only intentionally
- **Agent state is proof-classified** (`live` / `orphaned` / `undetermined`);
  only proven-orphaned entries can be selected, and only after the
  `--agent-state-grace` idle floor (24h default)
- **Guided Codex review** locks dirty, active, or recently used worktree
  units and keeps protected rows visible but unselectable
- **Home-scoped roots**: scans start at `$HOME`; `--root` only narrows to
  existing directories under it. Deletions outside `$HOME`, symlink escapes,
  and unvalidated paths are rejected
- **Protected content is read-only**: Codex session retention aggregates are
  inventory only — see [docs/PROTECTED_RETENTION.md](docs/PROTECTED_RETENTION.md)

The full safety model, guided policy ordering, cache-reuse identity checks,
and partial-scan behavior are specified in [docs/SPEC.md](docs/SPEC.md).

## Documentation

- [docs/JSON_SCHEMA.md](docs/JSON_SCHEMA.md) — versioned `scan --json`,
  `clean_plan`, and `clean_receipt` schemas, including `--include-paths`
  and `--receipt-file` behavior
- [docs/SPEC.md](docs/SPEC.md) — engineering spec: flag semantics, guided
  cleanup policy, safety boundaries
- [docs/CATEGORY.md](docs/CATEGORY.md) — category definitions and
  future store constraints
- [docs/PROTECTED_RETENTION.md](docs/PROTECTED_RETENTION.md) — the frozen,
  read-only protected-content retention surface
- [docs/DOGFOOD.md](docs/DOGFOOD.md) — real dogfood scan and dry-run
  transcripts validating release behavior
- [docs/WINDOWS.md](docs/WINDOWS.md) — Windows support contract
- [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) — 0.x compatibility and
  deprecation policy
- [docs/COMPLETIONS.md](docs/COMPLETIONS.md) — shell completions and man
  page: installation, uninstall, and regeneration

## Agent workflow

AI assistants can drive the same loop with JSON: scan, summarize by
project/category/age, run a dry-run plan, ask the user, then execute with
identical selectors (only `--dry-run` removed):

```bash
aibris scan --json
aibris clean --no-guide --category worktree --age 7d --dry-run
aibris clean --no-guide --category worktree --age 7d
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for
architecture and development guidelines. New tools can be added by
implementing the `DebrisProvider` interface.

## Roadmap

See [ROADMAP.md](ROADMAP.md). The project intentionally remains in the 0.x
series until the maintainer is satisfied; milestones are capability gates, not
promised release dates or an implied v1.0.0 schedule.

The [0.x compatibility and deprecation policy](docs/COMPATIBILITY.md) defines
which documented CLI and JSON contracts are stable during that period.

## License

MIT — see [LICENSE](LICENSE).
