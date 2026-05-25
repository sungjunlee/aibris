# Changelog

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
- README: `aibris prune` → `aibris clean`, `7d` → `168h`
- README expanded with English, features, safety section, usage examples
- `containsTool` no longer conflates "contains" with "match all"

## [0.1.0] - initial
- scan and clean commands
- 7 adapters: codex, claude, cursor, ai-logs, node_modules, build-cache, pip-cache
- age filtering, category filtering, tool filtering
- --dry-run, --interactive, --risky, --json modes
