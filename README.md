# aibris

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![CI](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml/badge.svg)](https://github.com/sungjunlee/aibris/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sungjunlee/aibris)](https://goreportcard.com/report/github.com/sungjunlee/aibris)

**AI coding agents fill your disk.** Every Codex, Claude Code, Cursor, or
Windsurf session can leave a Git worktree, a project-state store, logs, and a
fresh `node_modules` behind. aibris (AI + debris) finds that debris under
`$HOME`, shows how much of it is reclaimable and why, and deletes only what it
can prove is safe to remove — after a preview and your confirmation.

## Install

```bash
# macOS
brew install sungjunlee/tap/aibris

# macOS or Linux, into ~/.local/bin, no sudo
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash
```

Windows archives are experimental; see the
[Windows support contract](docs/WINDOWS.md). Other install options, the
Homebrew tap's trust model, and how to verify release artifacts are in
[docs/INSTALL.md](docs/INSTALL.md).

## Use

```bash
aibris scan             # what is taking space, and how much is reclaimable
aibris clean --dry-run  # the exact cleanup plan; nothing is deleted
aibris clean            # review the plan, confirm, then delete
```

`scan` shows what it found, what a default `clean` would reclaim, what is
held back and why, and how full the home volume is:

```text
summary
  found        94.2 MB in 3 items
  reclaimable  0 B by default (estimate)
  held back    20.0 KB younger than 7d
               94.2 MB AI logs (need --risky)
  volume       home (apfs): 92% used, 34.0 GB free, tight

by category
  ai-logs          1    94.2 MB
  node_modules     2    20.0 KB
```

`clean --dry-run` lists every target with its size, path, and the reason it
was chosen, plus everything it kept and why. Drop `--dry-run` to run the same
plan; aibris asks `Proceed? [y/N]` before deleting anything.

When several AI worktrees are worth reviewing, `clean` opens a guided review:
each worktree is shown as recommended, reviewable, or locked (dirty, recently
used, or with commits no branch keeps), and you toggle what to remove.

## What it finds

| Category | Examples | Cleaned by default |
| --- | --- | --- |
| AI worktrees | Codex, relay, gstack, and superpowers worktree containers, plus `$HOME` conventions such as `.tool/worktrees` | Orphaned ones; guided review recommends others from Git evidence |
| Agent state | Claude Code and Cursor project stores, Grok CLI session stores | Only when proven orphaned and idle for 24h |
| AI logs | Codex, Claude Code, and Windsurf logs | Only with `--risky` |
| Dependencies | project `node_modules` | Older than 7 days |
| Build caches | Go, npm and npx, Gradle, Cargo, Xcode, Homebrew, CocoaPods | Older than 7 days, any age when the home volume is over 95% full |
| Python caches | pip and uv | Same as build caches |

The first three rows are agent-produced state, aibris's reason to exist. The
rest is generic build debris, covered so one `scan` shows the whole picture.
Category definitions are in [docs/CATEGORY.md](docs/CATEGORY.md).

## How it decides what is safe

- **Preview, then confirm.** `--dry-run` only plans. A real `clean` asks
  before deleting; `--force` skips that prompt and nothing else.
- **Worktrees** whose parent repository still exists are kept unless you
  select them in guided review or pass `--include-active-worktrees`. Guided
  review locks dirty and recently used checkouts and any whose commits no
  branch keeps. Directories that are not valid linked worktrees are never
  removed.
- **Agent state** is removed only when the project directory it recorded is
  gone, and only after the idle floor (`--agent-state-grace`, 24h).
- **AI logs** need `--risky`.
- **Every deletion passes one gate**: the path must be inside `$HOME`, must
  not be a protected location or its ancestor (`~/Documents`, `~/Library`,
  `~/.ssh`, a tool's whole home, an agent store), and must not be a primary
  Git repository (a `.git` directory) or Git metadata.
- **Protected content is read-only.** Codex session retention is reported,
  never cleaned; see [docs/PROTECTED_RETENTION.md](docs/PROTECTED_RETENTION.md).

The full model is in [docs/SPEC.md](docs/SPEC.md) and
[docs/SECURITY_AUDIT.md](docs/SECURITY_AUDIT.md).

## Common commands

```bash
aibris scan --root ~/.codex            # narrow to part of $HOME
aibris scan --json                     # machine-readable (docs/JSON_SCHEMA.md)

aibris clean --age 30d                 # idle 30+ days (h, d, w, mo, y); agent state uses --agent-state-grace
aibris clean --category node_modules   # one category
aibris clean --tool codex,claude       # specific tools
aibris clean --risky                   # include AI logs
aibris clean --interactive             # confirm each item
aibris clean --guide                   # force guided worktree review
aibris clean --no-guide                # force the classic plan
aibris clean --strip                   # remove node_modules and build output inside kept worktrees
```

`aibris clean --help` lists every flag.

### Keeping things out

Hide paths from discovery with `--exclude <path-or-glob>`, a
`~/.config/aibris/ignore` file (or `$XDG_CONFIG_HOME/aibris/ignore`), or a
`.aibris-ignore` file at a scan root. Exclusions only hide; they never make
something cleanable, and patterns outside the scan roots are rejected.

`clean --protect-path <path>` keeps a checkout and every worktree or debris
item that contains it.

## For AI agents

Agents can drive the same loop through JSON: scan, summarize, show a dry-run
plan, ask the user, then run the same selectors without `--dry-run`. A
non-dry-run `--json` run also needs `--force` (after the user confirmed) or
`--interactive`.

```bash
aibris scan --json
aibris clean --no-guide --category worktree --age 7d --dry-run --json
aibris clean --no-guide --category worktree --age 7d --json --force
```

[`skills/aibris/SKILL.md`](skills/aibris/SKILL.md) packages this workflow as
an agent skill. Schemas are in [docs/JSON_SCHEMA.md](docs/JSON_SCHEMA.md).

## Documentation

- [docs/INSTALL.md](docs/INSTALL.md) — install paths, Homebrew trust, artifact verification
- [docs/WINDOWS.md](docs/WINDOWS.md) — Windows support contract
- [docs/JSON_SCHEMA.md](docs/JSON_SCHEMA.md) — `scan --json`, clean plan, and receipt schemas
- [docs/SPEC.md](docs/SPEC.md) — flag semantics, guided cleanup policy, safety boundaries
- [docs/CATEGORY.md](docs/CATEGORY.md) — category definitions
- [docs/PROTECTED_RETENTION.md](docs/PROTECTED_RETENTION.md) — the read-only retention surface
- [docs/COMPLETIONS.md](docs/COMPLETIONS.md) — shell completions and man pages
- [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) — what is stable during 0.x

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). Run
`make check` and `make test` before opening a pull request.

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md). The project stays on 0.x until the
maintainer is satisfied with the experience; the
[0.x compatibility and deprecation policy](docs/COMPATIBILITY.md) defines
which CLI and JSON contracts are stable meanwhile.

## License

MIT — see [LICENSE](LICENSE).
