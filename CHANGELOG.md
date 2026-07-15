# Changelog

## Unreleased

## [0.8.0] - 2026-07-13

### Added
- Guided cleanup now groups nested Git members into one physical cleanup unit,
  uses canonical repository identity for retention, and combines metadata-only
  Codex session, reflog, and filesystem fallback activity evidence.
- Git-aware active worktree removal preflights every member, preserves branch
  refs, cleans parent worktree metadata, and reports partial failures without
  overstating reclaimed bytes.

### Changed
- Cleanup recommendations now apply independent recent-activity, per-repository
  retention, idle-age, and size policies; protected-only Codex pressure still
  opens guided review with nothing preselected.
- Missing or gone upstream state is explanatory rather than a hard lock when a
  named ref makes the commit recoverable.

### Safety
- Dirty, unreadable, recently active, current-directory, and unreferenced
  detached worktrees remain locked. `--force` skips only final confirmation and
  never forces Git worktree removal or bypasses hard-safety checks.
- Controlled dogfood limited the live `$HOME` exercise to read-only inspection
  and dry-run planning, then verified branch-preserving removal with a
  disposable linked worktree under a temporary `HOME`.
- Cleanup documentation now keeps approved selectors, roots, age, routing, and
  safety flags identical between preview and execution, removing only
  `--dry-run` after approval.

## [0.7.0] - 2026-07-10

### Added
- Guided Codex worktree cleanup now has a checklist selection model for
  terminal use, separating recommended, reviewable, and locked rows.
- The guide shows projected freed space from normalized selected targets, so
  overlapping selections preview the same size the cleaner will act on.
- Age threshold commands in the guided flow can replan recommendations while
  preserving user deselect overrides where safety policy still allows them.

### Changed
- Low-risk recommendations remain selected by default, while hard-safety rows
  stay visible as locked rows and cannot be selected.
- Non-TTY and piped usage keep the line-oriented text fallback, including
  checkbox markers, locked-row markers, blank-input accept, abort, and dry-run
  no-delete behavior.
- Real deletion still exits the guide through the existing dry-run preview and
  final confirmation path unless the user explicitly passes `--force`.

## [0.6.1] - 2026-07-10

### Changed
- `clean` and `clean --dry-run` now open guided Codex worktree review by
  default when no classic cleanup selector is supplied and useful guided
  recommendations exist.
- `--no-guide` keeps the classic cleanup audit/executor route for scripts,
  explicit cleanup workflows, and users who do not want guided review.
- README, spec, skill workflow, and dogfood notes now present
  `aibris clean --dry-run` as the natural first cleanup preview.

## [0.6.0] - 2026-07-07

### Added
- `clean --guide` for guided Codex worktree cleanup. The guide defaults
  low-risk active Codex worktrees to selected, shows protected rows, supports
  number toggles and abort, and hands the final selection to the normal dry-run
  clean plan before deletion.
- Codex session activity indexing from metadata only, using session timestamps
  and working directories without reading conversation bodies.
- Conservative guided cleanup git safety checks for dirty worktrees, unpushed
  commits, unknown upstream comparisons, and the current working directory.
- Real local guided dry-run dogfood evidence in `docs/DOGFOOD.md`.

### Changed
- `skills/aibris/SKILL.md` now routes active Codex worktree bloat to
  `aibris clean --guide --dry-run` while preserving dry-run-before-delete
  rules.
- Guided cleanup planning now combines target deduplication, nested overlap
  protection, project freshness, activity signals, size thresholds, and git
  safety before recommending cleanup rows.

## [0.5.1] - 2026-06-26

### Changed
- HOME-wide scans now batch worktree and `node_modules` size estimation on Unix
  with `du -sk`, while retaining the Go walker as a fallback.
- Provider scan parallelism is tuned to reduce disk I/O contention during large
  HOME scans.

### Fixed
- Real HOME dogfood scan latency improved from 178.55s to 78.32s on the
  measured machine.

## [0.5.0] - 2026-06-25

### Added
- Worktree discovery now follows `$HOME` worktree directory conventions instead
  of relying on a fixed tool list, so hidden owners such as `.relay`,
  `.codex`, `.claude`, and future local tools can be detected when they expose
  `worktrees`, `worktree`, `worktree-*`, or `worktrees-*` roots.
- `scan --json` now includes a path-derived `source` field for worktree items,
  such as `.codex`, `.relay`, `.claude`, or `project-local`.
- `scan` and zero-candidate `clean` output now explain protected active
  worktrees, age-blocked items, risky items, and category/tool-filtered items.

### Changed
- Generic worktrees are now cleanable only after scanner validation proves they
  are active or orphaned Git worktrees under `$HOME`.
- Human-readable worktree names include the source owner for unknown tools,
  for example `.relay/1948review`.
- Worktree discovery is bounded to shallow scan-root containers to keep
  full-home scans practical.

### Fixed
- Cancelled worktree root scans now propagate the context error instead of
  allowing partial scan results to be treated as successful.

## [0.4.0] - 2026-06-14

### Added
- `clean` now shows scan progress before candidate filtering, so running
  cleanup without a prior scan no longer looks stalled.
- `scan` writes a short-lived last-scan snapshot, and `clean` reuses it for
  5 minutes when roots, cache schema, and freshness match.
- `clean` re-checks cached target paths before presenting or deleting them.

### Changed
- `clean --dry-run` and delete confirmation now share the same target plan
  renderer with category, size, project, age/status, path, and action.
- Target lists now use explicit `global` or `-` labels instead of ambiguous
  `?` placeholders.
- README now describes the tool's cleanup targets and scan-to-clean loop more
  directly.

### Fixed
- Long deletions now print per-item start progress before slow remove or
  cleanup-command work.
- Future-dated, stale, schema-mismatched, or root-mismatched scan snapshots are
  ignored and fall back to a live scan.

## [0.3.4] - 2026-06-06

### Fixed
- Installer now runs correctly when executed from stdin via `curl ... | bash`.
  The `0.3.3` installer guard could fail under `set -u` with
  `BASH_SOURCE[0]: unbound variable`.

## [0.3.3] - 2026-06-06

### Changed
- Installer now defaults to `~/.local/bin` so normal installs do not require
  administrator privileges.
- Installer only falls back to `sudo` for explicitly requested prefixes, such
  as `--prefix /usr/local/bin`.
- Installer prints shell-specific PATH guidance when the install directory is
  not currently available on `PATH`.
- `make install` now honors `PREFIX`, defaulting to `~/.local/bin`.

## [0.3.2] - 2026-06-06

### Added
- Human-readable `scan` now runs providers with bounded parallelism and shows
  interactive spinner progress on terminals.
- `clean` confirmation now prints a target plan with category, size, project,
  age/status, path, and cleanup command before asking for approval.
- Test coverage for spinner output and deterministic provider concurrency.

### Fixed
- `node_modules` entries found under workspace-style roots are now accepted by
  cleanup path safety validation instead of being rejected as unsafe.

## [0.3.1] - 2026-06-03

### Changed
- Installer now prefers GitHub `releases/latest/download` URLs for latest
  binaries and no longer falls back to source builds unless `main` is requested.
- GoReleaser archive names are stable across versions for API-free latest
  downloads.

## [0.3.0] - 2026-06-01

### Added
- `--age` now accepts human values such as `7d`, `2w`, `1mo`, and `1y`.
- `install.sh` for Homebrew-free installation from GitHub Releases or `main`.
- Unified `WorktreeAdapter` for Codex, Claude, and generic AI worktree discovery.
- Worktree health detection (`active`, `orphaned`, `plain-dir`).
- JSON schema documentation for `scan --json`.
- Security audit documentation.

### Changed
- README and project docs now focus on AI coding workflow debris.
- GoReleaser config updated for current v2 keys.
- GitHub Actions updated to current Node 24-compatible actions.
- Directory size estimation uses a bounded worker-pool walker.

### Fixed
- Symlink-aware cleanup path validation.
- Default scanner test no longer scans the real home directory.
- CI no longer depends on a Go-version-incompatible golangci-lint binary.

## [0.2.0] - 2026-05-25

### Added
- `--version` flag showing version 0.2.0
- `--force` / `-f` flag to skip confirmation prompt
- Confirmation prompt before deletion (unless `--force` or `--dry-run`)
- `--age <1h` warning for very short age values
- Signal handling (Ctrl+C) via `signal.NotifyContext` for graceful cancellation
- Context propagation to `estimateDirSize` for responsive cancellation during large scans
- MIT LICENSE file
- CHANGELOG.md
- CONTRIBUTING.md and community health files

### Changed
- `containsTool` now returns `false` for empty list (caller handles all-match logic)
- `FormatSize` has bounds check for extremely large sizes
- `DryRun` uses human-friendly age format (`today`/`Nd ago`) instead of raw Go duration
- `interactiveClean` uses `bufio.Scanner` instead of `fmt.Scanln` for robust input handling
- Root command `Short`/`Long` updated to reflect full scope (caches, node_modules, logs)
- `clean --help` lists all valid categories and tools
- `Execute` accumulates errors (returns partial failure info)
- No-result messages updated ("No items to clean", "No AI tool debris found")

### Fixed
- README: `aibris prune` → `aibris clean`, duration examples clarified
- README expanded with English, features, safety section, usage examples
- `containsTool` no longer conflates "contains" with "match all"

## [0.1.0] - initial
- scan and clean commands
- 7 adapters: codex, claude, cursor, ai-logs, node_modules, build-cache, pip-cache
- age filtering, category filtering, tool filtering
- --dry-run, --interactive, --risky, --json modes
