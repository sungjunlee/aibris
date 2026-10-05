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
make test        # go test ./...
make test-race   # go test -race ./...
make dist        # goreleaser snapshot build
```

`make check` takes about ten seconds and is what CI's check job runs.

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
