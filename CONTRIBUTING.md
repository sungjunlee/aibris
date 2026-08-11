# Contributing to aibris

## Getting Started

```bash
git clone https://github.com/sungjunlee/aibris.git
cd aibris
make build
./aibris scan
```

## Development

```bash
make build    # go build -o aibris .
make test     # go test ./...
make lint     # go vet ./...
make tidy     # go mod tidy
make dist     # goreleaser release --snapshot --clean
```

## Architecture

See [AGENTS.md](AGENTS.md) for the full architecture overview and development rules.

```
cmd/         → cobra commands (root, scan, clean)
internal/
  adapter/   → DebrisProvider interface + codex, claude, etc.
  scanner/   → Scan(): iterates all adapters, collects results
  cleaner/   → Filter(): applies category-specific eligibility and selectors, Execute()
  types/     → DebrisInfo, ScanResult, PruneOptions
```

## Adding a New Adapter

1. Create `internal/adapter/<name>.go` implementing `DebrisProvider`
2. `Name()` returns kebab-case Tool constant
3. `Scan()` respects context cancellation
4. Use `estimateDirSize(ctx, path)` for size calculation
5. Register in the `internal/adapter/providers.go` `providers` slice
6. For an adapter whose `Category()` is `agent-state`, also implement `AgentStateRevalidator`; classification is proof-based from the recorded cwd, while `--agent-state-grace` only gates default selection (the classic `--age` filter does not apply), and cleanup refuses entries without a registered revalidator
7. Report `ModTime` as the later of the path's own mtime and `NewestTreeModTime(ctx, path)` when the container's own mtime is not the activity signal (cache trees, agent-state stores), and always set `PathModTime` to the path's own mtime in that case — leaving it empty makes the cleanup preflight overwrite `ModTime` with the container mtime
8. Add tests in `internal/adapter/<name>_test.go`

## Before Submitting

- `make lint` passes
- `make test` passes
- New adapters have tests
- Run `make tidy` if adding imports

## License

MIT — see [LICENSE](LICENSE).
