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
  adapter/   → WorktreeProvider interface + codex, claude, etc.
  scanner/   → Scan(): iterates all adapters, collects results
  cleaner/   → Filter(): filters by age/category/tool, DryRun(), Execute()
  types/     → WorktreeInfo, ScanResult, PruneOptions
```

## Adding a New Adapter

1. Create `internal/adapter/<name>.go` implementing `WorktreeProvider`
2. `Name()` returns kebab-case Tool constant
3. `Scan()` respects context cancellation
4. Use `estimateDirSize(ctx, path)` for size calculation
5. Register in `internal/scanner/scanner.go` `providers` slice
6. Add tests in `internal/adapter/<name>_test.go`

## Before Submitting

- `make lint` passes
- `make test` passes
- New adapters have tests
- Run `make tidy` if adding imports

## License

MIT — see [LICENSE](LICENSE).
