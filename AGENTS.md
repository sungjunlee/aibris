# AGENTS.md

Shared guidance for any AI agent (Claude Code, Codex, and others) working on
this repository. `CLAUDE.md` is a symlink to this file.

Current contributor/safety guidance is maintained here. [docs/SPEC.md](docs/SPEC.md)
is the CLI and execution contract; [docs/JSON_SCHEMA.md](docs/JSON_SCHEMA.md)
is the stable JSON contract; [docs/INSTALL.md](docs/INSTALL.md) and
[docs/WINDOWS.md](docs/WINDOWS.md) cover release/install trust and native platform
assurance. Dated design and dogfood notes are historical evidence, not overrides
of these contracts.

## Project

aibris (AI + debris) is a Go CLI that finds and removes the disk debris AI
coding agents leave under `$HOME`: Git worktrees, agent project stores, and
agent logs, plus generic build debris (`node_modules`, build and package
caches) so one scan shows the whole picture. It deletes files, so safety
matters more than coverage or speed.

## Product direction

### What aibris should do

- Make cleanup boring and reviewable: `scan`, then `clean --dry-run`, then
  `clean` with a confirmation. Every target shows its size, path, and reason;
  everything kept shows why.
- Delete only what it can justify from evidence: a worktree whose parent
  repository is gone, an agent store whose recorded project directory is gone,
  a cache that is rebuildable by definition.
- Prefer fail-closed: when evidence is missing, unavailable, or ambiguous,
  keep the item and say so.
- Stay focused on what AI agents produce. New agent tools appear constantly;
  covering their worktrees, stores, and logs is the core of the product.
- Keep output dense and terminal-native, and keep `--json` stable for agents
  that drive the CLI (see `skills/aibris/SKILL.md`).

### What aibris should not do

- Do not compete with general-purpose cleaners on generic caches. Cover them
  for a complete picture, but do not add cache targets for their own sake.
- Do not delete agent project stores without proof that the owning project is
  gone. Logs and archived sessions are cleaned only behind `--risky`.
  Protected retention stores are read-only.
- Do not call GitHub or any network service to decide what is safe.
- Do not add a flag, environment variable, or config key to resolve one edge
  case. A new knob carries the same weight as a new setting: state the
  fix-by-default alternative and why it fails before adding one.
- Do not add background daemons, schedulers, or automatic cleanup.

### Decision filter for new features

1. Does it reclaim space that AI-agent work produces, or complete the picture
   of one home directory?
2. Is it safe by default, previewable with `--dry-run`, and explainable in one
   terminal screen?
3. Is the target rebuildable, or backed by evidence that its owner is gone?
4. Can it be tested hermetically, without the maintainer's real home?

If the answer is no or unclear, narrow the feature or decline it.

## Repository map

```
main.go
cmd/                  cobra commands (root, scan, clean), flags, terminal I/O
internal/
  adapter/            DebrisProvider implementations and provider registry
  scanner/            runs providers, aggregates results, normalizes roots
  scancache/          last-scan snapshot persistence and path identity
  exclude/            --exclude, ignore files, --protect-path matching
  cleaner/            eligibility, filtering, overlap safety, classic execution
  worktree/           worktree units, Git evidence, guided policy, execution, strip
  executor/           prepared-target orchestration and execution receipts
  safedelete/         the only package that may recursively delete
  cleanjson/          JSON plan and receipt documents
  cleancommand/       clean route selection
  scanreport/         human and JSON scan rendering
  codexhome/          Codex home resolution (CODEX_HOME, AIBRIS_CODEX_HOMES)
  codexsession/       Codex session metadata reader
  codexactivity/      Codex session-activity index for worktree activity
  retention/          read-only protected retention inventory
  volume/             home-volume capacity and filesystem type
  apfs/               local APFS snapshot list and thin (macOS)
  pathidentity/       platform file identity for TOCTOU checks
  types/              DebrisInfo, ScanResult, PruneOptions
  testutil/           hermetic HOME and environment helpers
test/                 black-box CLI, install, docs, and Homebrew script tests
tools/                release-asset generator and performance harness
skills/aibris/        agent skill that drives the CLI through JSON
docs/                 user documentation, spec, and design notes
```

## Commands

```bash
make build       # go build -o aibris .
make check       # gofmt, go mod tidy -diff, vet, staticcheck, govulncheck, shellcheck
make test        # root and tools/perfharness module tests
make test-race   # both modules with the race detector
./aibris scan --root <dir-under-home>
./aibris clean --dry-run
```

Run `make check` and the relevant tests before every commit. CI runs the same
check job plus tests on Linux, macOS, and Windows.

## Safety invariants

These hold for every change. Breaking one is a bug even if tests pass.

- **One deletion gate.** Only `internal/safedelete` may call `os.RemoveAll`
  (an architecture test enforces it). Every removal, cleanup command, and
  `git worktree remove` passes `safedelete.Check`: canonical path strictly
  inside `$HOME`, not a protected location or its ancestor, not a primary Git
  repository (a `.git` directory) or Git metadata. Add new protected
  locations there, not in providers.
- **Re-verify at the mutation boundary.** Scan results can come from a cache.
  Executors re-check identity, age, Git state, agent-state classification,
  and overlap right before mutating, and refuse on drift or incomplete
  activity evidence. Orphaned worktrees need fresh unchanged member/regular
  `.git` marker evidence, including still-missing gitdirs; symlinked markers
  cannot authorize removal. Prepared typed evidence stays attached to the
  selected target through execution and receipt projection. Guided and JSON
  receipts share typed prepared-target identity binding; missing or duplicate
  guided outcome identities are invariant errors. Errors found after mutation
  preserve known outcomes in a failed receipt rather than suppressing it.
- **Never trust inventory for authority.** A path from a scan or cache is a
  claim; execution re-derives whether it may be removed (see strip).
- **Preview and confirm.** `--dry-run` never mutates. A real `clean` prompts;
  `--force` skips only the prompt, never a safety check. `--interactive`
  confirms per item. All prompts share the run's stdin line reader, created in
  `cmd`; cancellation stops the run and permanently disables that reader.
- **Defaults:** classic `--age` is `7d` (caches on a home volume over 95% full
  relax it); AI logs need `--risky`; active worktrees need explicit selection;
  orphaned agent state ignores `--age` and waits for `--agent-state-grace`
  (24h).
- **`--exclude` only hides** from discovery and never matches ancestors.
  **`--protect-path`** is clean-only and protects every outer owner that
  contains the path.

## Adding a provider

1. Implement `DebrisProvider` in `internal/adapter/<name>.go` and register it
   in `defaultProviders` in `internal/adapter/providers.go`.
2. `Scan()` honors context cancellation and measures sizes with
   `estimateDirSize()`.
3. For nested cache trees and agent stores, report the newest in-tree mtime as
   `ModTime` and always set `PathModTime` to the path's own mtime; otherwise
   cleanup preflight overwrites `ModTime` with the container mtime. Providers
   whose container mtime is the activity signal (`node_modules`) skip this.
4. Projects that are subdirectories of a container use `detectProjectName()`
   (hidden directories excluded). Stores whose recorded cwd names the project
   use `projectNameFromRecordedCWD()` and never touch the filesystem.
5. An `agent-state` provider must also implement `AgentStateRevalidator`;
   cleanup refuses agent-state items without one. Classification is proof
   based (`live` / `orphaned` / `undetermined`); `--age` does not apply.
6. Make sure every target path passes `safedelete.Check`, and add hermetic
   tests (`testutil.SetHome`) in `internal/adapter/<name>_test.go`.

Rebuildable caches do not need a provider: add an entry to `cacheCatalog` in
`internal/adapter/cache_catalog.go` instead.

Known gap: `types.Tool` mixes vendors with provider names (the worktree
provider reports `codex` for every tool).

## Worktree discovery invariants

- Known deep containers come only from a finite registry: `~/.codex/worktrees`
  (per Codex home from `$CODEX_HOME` and `$AIBRIS_CODEX_HOMES`),
  `~/.relay/worktrees`, `~/.gstack/worktrees`, `~/.config/superpowers/worktrees`.
- The convention fallback looks under `$HOME` for directories named
  `worktrees`, `worktree`, `worktree-*`, `worktrees-*`, `*-worktree`, or
  `*-worktrees`, up to `maxWorktreeContainerDepth = 4`.
- Once a valid linked member is found, sibling checkouts of the same repository
  under the scan roots are added from `.git/worktrees/*/gitdir`, only when the
  checkout's `.git` points back to that admin entry. Primary checkouts, missing
  or prunable paths, paths outside the roots, and owners already visited are
  skipped. Never recurse all of `$HOME` looking for repositories.
- Hidden owners (`.codex`, `.something`) can hold worktrees; do not prune them
  for being hidden, but only check their immediate convention children.
- A candidate needs a `<entry>/.git` or `<entry>/<project>/.git` file.
  Registered containers also allow `<owner>/<leaf>/<checkout>/.git`.
- One outer `<entry>` is one physical mutation owner. Mixed valid and invalid
  markers make the whole owner a review-only `plain-dir`. An empty leftover
  member is not an invalid marker. Registered sidecars (currently only
  `.orca-worktree-trash`) are skipped during member classification.
- Missing, empty, malformed, or directory markers produce a review-only
  `plain-dir` with an explicit reason; I/O failures are provider errors.
- `.git` `gitdir:` decides `active` vs `orphaned`; a missing gitdir is
  `orphaned`. `plain-dir`, empty, and unknown statuses are never cleanup
  candidates regardless of flags.
- An explicit `--root` is a hard boundary. Extra Codex homes are added only to
  the default `$HOME` scan; an explicit root that excludes them gets a
  one-line diagnostic.

## Default targets

| Provider | Category | Default clean | Paths |
| --- | --- | --- | --- |
| codex (worktree provider; reports `codex` for every tool) | worktree | orphaned only in the classic plan; guided review may recommend active ones from Git evidence | registry and convention containers above |
| claude | agent-state | proven orphaned, after grace | `~/.claude/projects/<name>/` |
| cursor | agent-state | proven orphaned, after grace | `~/.cursor/projects/<name>/` |
| grok | agent-state | proven orphaned, after grace | `~/.grok/sessions/<url-encoded cwd>/`; the name must agree with every session's `prompt_context.json` `working_directory` |
| windsurf | ai-logs | `--risky` only | `~/.codeium/windsurf/` |
| ai-logs | ai-logs | `--risky` only | `$CODEX_HOME/logs_2.sqlite`, `$CODEX_HOME/archived_sessions/`, `~/.claude/command-audit.log`, `~/.claude/file-history/` |
| node_modules | node_modules | older than `--age` | `node_modules` under scan roots, noisy trees pruned |
| build-cache | build-cache | older than `--age` | effective `GOCACHE`; Gradle, npm (`_cacache`, `_npx`), and Cargo registry caches; Xcode caches and DerivedData; Homebrew cache (verified cache path removal); CocoaPods cache; `~/.dartServer/` |
| pip-cache | other-cache | older than `--age` | pip and uv caches |

Cache locations come from `internal/adapter/cache_catalog.go` and follow
each tool's platform defaults. An override variable is honored only when the
directory it names marks itself as a cache with a valid `CACHEDIR.TAG` (today
`UV_CACHE_DIR`); otherwise an override could make an ordinary directory a
cleanup target. Adding a
rebuildable cache is one catalog entry; the cleanup allowlist accepts every
path the catalog resolves to. Never add a cache whose wholesale removal can
break installed projects (stores that projects link into). Cleanup commands
are re-derived from the live catalog (tool, category, canonical target, argv,
and pinned cache environment); recipe drift refuses execution and fallback.
A missing authorized executable may fall back only to gated removal of the
scanned path. Homebrew cleanup uses gated removal of the verified cache path,
without a package-manager cleanup command.

## Code rules

- No speculative abstraction; interfaces only at real extension points.
- Handle errors that can happen; fail closed on safety decisions.
- Match surrounding style; `gofmt`; tabs. Run `go mod tidy` after adding a
  dependency.
- Change only what the task needs; report unrelated problems instead of fixing
  them in the same change.

## Testing rules

- Tests are hermetic: use `testutil.SetHome` and `t.TempDir()`; never read the
  developer's real home or depend on host disk fullness.
- Do not write tests that pin source layout (which file defines what). Pin
  behavior, or a real invariant such as the deletion-gate architecture test.
- Synthetic paths must not start with `/home/` (an autofs mount on macOS that
  makes every stat slow); use `/aibris-test-home/...` or a temp dir.
- Git fixtures are slow; reuse helpers in `git_fixture_test.go` and keep
  per-test repositories small.

## Workflow

1. Understand the problem and state the plan in a sentence or two.
2. Implement the smallest change that solves it, with tests.
3. `make check` and the affected tests pass before committing.
