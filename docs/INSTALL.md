# Installing aibris

This page covers every install path and how to verify what you downloaded.
The short version is in the [README](../README.md#install).

## Platforms

- **macOS**: first-class. The recommended install is the Homebrew tap below.
  `install.sh` remains the checksummed Homebrew-free path.
- **Linux**: first-class via `install.sh`. No `sudo` needed by default.
- **Windows**: experimental archives. See the canonical
  [Windows support contract](WINDOWS.md) for native installation, tested
  behavior, and unaudited boundaries. `install.sh` remains Unix/Bash-only.

## Install paths

### macOS (Homebrew)

```bash
brew install sungjunlee/tap/aibris
```

This is a **third-party tap** owned by `sungjunlee`, repository
https://github.com/sungjunlee/homebrew-tap. It is not reviewed by
`homebrew/core`. The fully-qualified command trusts **that formula**
(Homebrew 6.0 item trust), not the whole tap. Do not run
`brew trust sungjunlee/tap`.

The formula `sha256` is published by the same publisher as `checksums.txt`
(TOFU, not a second signer).

On Apple Silicon the binary lands in `$(brew --prefix)/bin` (usually
`/opt/homebrew/bin`). That is not `/usr/local/bin`. The formula also installs
bash, zsh, and fish completions plus man pages into the Homebrew prefix.
Homebrew's standard setup (`eval "$(brew shellenv)"` in `.zprofile`) puts
Homebrew `site-functions` on `fpath` before `compinit`, so the brew zsh
completion needs no extra `.zshrc` line. `install.sh` still writes only the
installing user's `~/.local` (and fish `~/.config`) files; see
[completions and man pages](COMPLETIONS.md).

Sharing a Mac does not make aibris multi-user. Keep these facts separate:

1. whether `aibris` is on that account's `PATH`
2. who owns the Homebrew prefix and can `brew upgrade`
3. which `$HOME` aibris will scan (the account that runs `aibris`)

### Without Homebrew

`install.sh` remains the unsigned `main` script; only the downloaded archive is
checked against `checksums.txt`.

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash
```

Install from the current main branch when you want unreleased changes:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- main
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- 0.12.0
```

The installer downloads GitHub Release binaries and verifies `checksums.txt`.
Both installers stage a replacement beside the destination before the final
rename. Failed download, checksum, staging, or replacement leaves an existing
binary intact; fixtures exercise these failure paths without real downloads.
The PowerShell installer additionally refuses a locked binary; see
[WINDOWS.md](WINDOWS.md).

The default install path uses GitHub's `releases/latest/download` URLs for
prebuilt binaries. `main` builds from source with Go. Release binaries are
built with the Go version pinned in CI. When you build from source, use Go
1.26.9 or later, or 1.27.2 or later on the 1.27 line: `go.mod` sets only a
minimum, and Go 1.27.0 and 1.27.1 still carry GO-2026-6604 (`os.Root` could
follow Windows junctions out of the root). `make check` runs `govulncheck`
against the toolchain in use and fails on an affected one.

By default, aibris installs to `~/.local/bin` and does not require `sudo`. If
that directory is not on your `PATH`, the installer prints the exact command to
add it for your shell. To install into a shared prefix instead:

```bash
curl -fsSL https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.sh | bash -s -- --prefix /usr/local/bin
```

## Verify release artifacts

Release archives ship `checksums.txt` (verified by `install.sh`), an SPDX SBOM
(`<archive>.sbom.json`), and a GitHub artifact attestation produced by the release
workflow. The current [release verification contract](SPEC.md#verification)
requires the tag event's same-SHA checks to succeed before creating the draft;
attestation must succeed before publication and the tap update. Local workflow
tests do not establish that a real release was published. Copy-paste
verification for a downloaded archive:

```bash
# Attestation: binds the archive to the release workflow build
gh attestation verify aibris_darwin_arm64.tar.gz --owner sungjunlee

# Checksums (same file install.sh checks)
sha256sum -c checksums.txt --ignore-missing

# SBOM published alongside the archive
syft convert aibris_darwin_arm64.tar.gz.sbom.json -o spdx-json
```
