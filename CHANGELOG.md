# Changelog

## Unreleased

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
