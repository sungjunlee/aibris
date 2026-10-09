# Contributing to aibris

Thanks for helping. aibris deletes files, so the bar for safety is high and
the bar for new flags is higher; read the product direction in
[AGENTS.md](AGENTS.md) before proposing a feature.

## Getting started

Start with a local checkout and run the commands below from its repository
root. Required tools are Go 1.26.9 or a later patched release (see `go.mod`
and [docs/INSTALL.md](docs/INSTALL.md)), Git, Make, a POSIX shell,
and shellcheck. `make check` downloads pinned staticcheck and govulncheck tools
through Go when missing; initial dependency/tool downloads and vulnerability
queries need network access. Tests use local fixtures rather than network
services. GoReleaser and syft are release-only tools; installation and release
trust are maintained in [INSTALL.md](docs/INSTALL.md).

```bash
make build
./aibris --help
./aibris scan --help
./aibris clean --help
```

For a read-only scan smoke test, use a synthetic HOME and a clean subprocess
environment so vendor homes and cache overrides cannot escape the fixture:

```sh
fixture=$(mktemp -d)
mkdir -p "$fixture/home/project/node_modules" "$fixture/tmp"
printf 'synthetic dependency\n' > "$fixture/home/project/node_modules/example.txt"
env -i PATH="$PATH" HOME="$fixture/home" USERPROFILE="$fixture/home" \
  LOCALAPPDATA="$fixture/home/.cache" XDG_CACHE_HOME="$fixture/home/.cache" \
  TMPDIR="$fixture/tmp" TEMP="$fixture/tmp" TMP="$fixture/tmp" \
  ./aibris scan --root "$fixture/home" --json
rm -r "$fixture"
```

Do not run a real-home cleanup to verify onboarding; inspect `clean --help`.
Native Windows uses PowerShell and an isolated profile instead of this POSIX
recipe; see [WINDOWS.md](docs/WINDOWS.md) for its CI coverage and limitations.

## Development

The root and `tools/perfharness` are separate Go modules. Root `go test ./...`
does not test the nested module. Run both:

```bash
make check
go test ./...
go -C tools/perfharness run . --help
go -C tools/perfharness test ./...
```

`make test` also runs both modules with a 30-minute test timeout. CI's
Linux/macOS jobs run both with the race detector, plus vet/staticcheck.
Windows CI runs the complete `internal/safedelete`, `internal/pathidentity`,
and `internal/testutil` packages in an isolated profile, selected native CLI
and installer fixtures, and the nested harness tests. Symlink fixtures report
a skip reason if the runner lacks symlink privilege. The
[perfharness guide](tools/perfharness/README.md) explains its historical
retention A/B scope and optional temporary build/scan test.

Report each verification command, exit code, and host OS/architecture. A
skipped test is not a pass on that platform: record the skip reason. Explicitly
mark native Windows, PowerShell, native arm64, or release publication **not
run** when absent. Cross-compilation, local workflow-graph tests, and mocked
installer/publication fixtures do not establish native execution or successful
GitHub Actions publication. CI verifies the exact tag SHA before release
publication; the ordering contract is in [SPEC.md](docs/SPEC.md#verification).

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
