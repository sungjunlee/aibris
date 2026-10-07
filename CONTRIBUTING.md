# Contributing to aibris

Thanks for helping. aibris deletes files, so the bar for safety is high and
the bar for new flags is higher; read the product direction in
[AGENTS.md](AGENTS.md) before proposing a feature.

## Getting started

```bash
git clone https://github.com/sungjunlee/aibris.git
cd aibris
make build
./aibris scan
./aibris clean --dry-run
```

## Development

```bash
make check       # gofmt, go mod tidy -diff, vet, staticcheck, govulncheck, shellcheck
make test        # root and tools/perfharness module tests
make test-race   # both modules with the race detector
make dist        # goreleaser snapshot build
```

`make check` is what CI's check job runs. Its vet/staticcheck checks and
`make test` cover both the root module and `tools/perfharness`; Linux/macOS CI
runs both modules with the race detector and vet/staticcheck. Windows CI runs
the complete `internal/safedelete`, `internal/pathidentity`, and
`internal/testutil` packages in an isolated profile. Symlink fixtures report a
skip reason if the runner lacks symlink privilege.

## Architecture

[AGENTS.md](AGENTS.md) has the repository map, the safety invariants every
change must keep, and the rules for adding a provider or touching worktree
discovery. [docs/SPEC.md](docs/SPEC.md) specifies flag semantics and the
guided cleanup policy.

## Adding support for a new AI tool

Open an issue with the "adapter request" template first, with the paths the
tool writes, what each one holds, and how you know a path is safe to delete
(for example, a recorded project directory that no longer exists). Then follow
"Adding a provider" in [AGENTS.md](AGENTS.md).

## Before submitting

- `make check` and `make test` pass.
- New behavior has hermetic tests (`testutil.SetHome`, temp directories).
- Every new delete target passes `safedelete.Check`.
- User-visible changes have a `CHANGELOG.md` entry under `[Unreleased]`.

## License

MIT — see [LICENSE](LICENSE).
