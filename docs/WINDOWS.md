# Windows support

Windows archives are **experimental**. This document is the support contract
for native Windows builds of aibris. It describes what the project tests today;
it is not a promise that every adapter or cache location works on Windows.

The project does not publish a minimum or supported Windows-version matrix.
`windows-latest` below means the current GitHub Actions runner image, not every
Windows release.

## Tested release surface

Pull-request CI uses an amd64 `windows-latest` runner to:

- build and run `aibris.exe`;
- exercise `--version`, `--help`, and an isolated `scan --json` without Bash;
- isolate `USERPROFILE`, `HOME`, cache, and temporary-directory variables;
- accept a scan root contained by that profile while rejecting a
  sibling-prefix root and a root outside the profile;
- verify that scan results do not escape the isolated profile;
- exercise Windows recorded-CWD volume lookup and its fail-closed error path;
  and
- reject reusable cleanup-target identities for Windows reparse points.

GoReleaser cross-builds zip archives for both Windows `amd64` and `arm64`, and
lists both in `checksums.txt`. The Windows runner builds and runs an amd64
executable from source. It does not natively run the arm64 archive, which
remains a cross-built experimental artifact.

Native binaries are expected to be invoked directly from PowerShell or Command
Prompt. A Unix shell, Bash wrapper, or translated Unix path is not part of the
native runtime contract.

## Install on native Windows

### PowerShell installer

`install.ps1` is a native PowerShell installer that downloads and verifies
releases without requiring Bash or administrator privileges:

```powershell
# Download and run (latest version)
irm https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.ps1 | iex

# Or download the script and run locally
Invoke-WebRequest `
    -Uri "https://raw.githubusercontent.com/sungjunlee/aibris/refs/heads/main/install.ps1" `
    -OutFile "install.ps1"
.\install.ps1
.\install.ps1 -Version 0.12.1
.\install.ps1 -Prefix "$env:USERPROFILE\bin"
.\install.ps1 -AddToPath
```

The installer:
- Downloads the release archive and verifies its SHA-256 checksum against
  `checksums.txt`
- Stages the download in a temporary directory, then replaces the existing
  binary atomically
- Preserves an existing installation if checksum verification fails or if the
  binary is locked (in use)
- Defaults to `$env:LOCALAPPDATA\Programs\aibris` (no admin required)
- Shows PATH setup guidance; add `-AddToPath` to update the user PATH automatically

Run `.\install.ps1 -Help` for all options.

### Manual installation

`install.sh` is a Unix/Bash installer and is unsupported on native Windows.
Install a release manually if the PowerShell installer is not suitable:

1. Open the release on
   [GitHub Releases](https://github.com/sungjunlee/aibris/releases).
2. Download `checksums.txt` and the zip matching the machine:
   `aibris_windows_amd64.zip` or `aibris_windows_arm64.zip`.
3. In PowerShell, verify that the archive's SHA-256 hash is exactly the value
   published in `checksums.txt`. For example, after replacing the version and
   architecture:

   ```powershell
   $version = "vX.Y.Z"
   $arch = "amd64" # or "arm64"
   $archive = "aibris_windows_${arch}.zip"
   $baseUrl = "https://github.com/sungjunlee/aibris/releases/download/$version"

   Invoke-WebRequest "$baseUrl/$archive" -OutFile $archive
   Invoke-WebRequest "$baseUrl/checksums.txt" -OutFile "checksums.txt"

   $pattern = '\s+' + [regex]::Escape($archive) + '$'
   $matches = @(Get-Content .\checksums.txt | Where-Object { $_ -match $pattern })
   if ($matches.Count -ne 1) {
       throw "Expected exactly one checksum for $archive"
   }
   $expected = ($matches[0] -split '\s+')[0]
   $actual = (Get-FileHash ".\$archive" -Algorithm SHA256).Hash
   if ($actual -ne $expected) {
       throw "SHA-256 mismatch for $archive"
   }
   ```

4. Extract the archive and place `aibris.exe` in a dedicated directory. The
   following example uses the current user's local application-data directory:

   ```powershell
   $installDir = Join-Path $env:LOCALAPPDATA "Programs\aibris"
   New-Item -ItemType Directory -Force $installDir | Out-Null
   Expand-Archive ".\$archive" -DestinationPath $installDir -Force
   ```

5. Add that directory to the user `PATH`, then open a new terminal. This
   PowerShell snippet updates the user `PATH` without requiring administrator
   privileges:

   ```powershell
   $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
   $entries = @($userPath -split ';' | Where-Object { $_ })
   if ($entries -notcontains $installDir) {
       [Environment]::SetEnvironmentVariable(
           "Path",
           (($entries + $installDir) -join ';'),
           "User"
       )
   }
   $env:Path = "$installDir;$env:Path"
   aibris.exe --version
   ```

## Paths and shells

Pass native absolute paths to `--root`. In PowerShell, write a profile-contained
root like this:

```powershell
aibris.exe scan --root "$env:USERPROFILE\workspace"
aibris.exe scan --root "$env:USERPROFILE\.codex" --json
```

In Command Prompt the corresponding environment expansion is
`%USERPROFILE%\workspace`.

aibris recognizes `~` and the Unix-style `~/...` form itself. It does not
recognize `~\...` as a portable `--root` form, so PowerShell examples use the
expanded `$env:USERPROFILE\...` absolute path. A root must exist, be a
directory, resolve inside the current user home, and not merely share its
string prefix.

WSL is a separate Unix environment. When installing and running aibris inside
WSL, use the Unix/Linux archive or `install.sh`, Linux home paths, and the Unix
instructions in the README. Those instructions do not describe native
`aibris.exe` behavior.

## Audited behavior and boundaries

The experimental Windows CI surface audits these behaviors:

- **Home containment:** default scans and explicit roots are constrained to
  the resolved user home. Cleanup performs its own containment and target
  checks before mutation.
- **`node_modules`:** recursive discovery under an accepted root is exercised
  with an isolated Windows profile. The normal directory-pruning and cleanup
  safety rules still apply.
- **Claude and Cursor agent state:** the implemented `.claude\projects` and
  `.cursor\projects` fixtures exercise recorded-CWD parsing and classification.
  Missing, unreadable, malformed, volume-ambiguous, or otherwise unverifiable
  evidence stays `undetermined` and is not cleaned. The normal cleanup path
  requires a fresh orphaned classification before mutation, but native CI does
  not separately exercise that end-to-end mutation route.
- **Cached cleanup evidence:** Windows file identity is checked again before
  reuse. Reparse-point targets do not receive a reusable cache identity, so an
  identity lookup error cannot authorize deletion.

Worktree discovery has narrower assurance. The implementation is built and
vetted on Windows, and its bounded registered-container, convention-discovery,
and Git-metadata rules are covered by the full cross-platform test suite on
Linux and macOS. Native Windows CI does not currently create real vendor
worktrees or claim that their on-disk layouts match those fixtures. Where a
candidate is found, it still needs valid direct or one-level nested Git
worktree metadata; active worktrees stay protected, orphaned worktrees are
reviewable cleanup candidates, and `plain-dir` entries are never cleaned.

## Reproducing unaudited scenarios

Issue [#550](https://github.com/sungjunlee/aibris/issues/550) provides isolated
reproduction fixtures for Windows-specific scenarios that are not yet audited
in native CI:

1. **Synthetic Git linked worktrees:** `test/windows_worktree_fixtures_test.go`
   creates `.git` pointer files and `gitdir` references using Windows absolute
   paths, verifying that `active`, `orphaned`, and `plain-dir` classifications
   work with Windows path separators. These fixtures do NOT require a Git
   installation and do NOT claim to match real vendor worktree layouts.

2. **Agent-state end-to-end cleanup:** `test/windows_agent_state_e2e_test.go`
   creates synthetic Claude `session.jsonl` and Cursor `worker.log` files with
   Windows recorded-cwd paths, verifying classification but NOT actual mutation.
   The test documents that Windows CI exercises classification logic but does
   not separately verify the cleanup mutation route.

3. **Volume ambiguity:** The agent-state test includes a fixture with a
   network-share path to confirm that unverifiable volume IDs result in
   `undetermined` classification and never authorize cleanup.

To reproduce these scenarios locally on Windows:

```powershell
# Run Windows-specific worktree fixture tests
go test -v ./test -run '^TestWindowsLinkedWorktreeFixture$'
go test -v ./test -run '^TestWindowsOrphanedWorktreeFixture$'
go test -v ./test -run '^TestWindowsPlainDirWorktreeFixture$'

# Run Windows-specific agent-state fixture tests
go test -v ./test -run '^TestWindowsClaudeAgentStateFixture$'
go test -v ./test -run '^TestWindowsCursorAgentStateFixture$'
go test -v ./test -run '^TestWindowsAgentStateCleanupEndToEndUnaudited$'
go test -v ./test -run '^TestWindowsRecordedCWDVolumeAmbiguity$'
```

These fixtures establish the shape expected by adapters and confirm that
Windows path handling works with synthetic metadata. They do NOT verify that
real vendor installations (Codex, Claude, Cursor worktree containers on Windows)
match these layouts, and they do NOT exercise actual file deletion.

## Unaudited areas

These areas are unsupported or unaudited:

- The native installation workflow is manual; `install.sh` does not install or
  update `aibris.exe`.
- The Go build cache is discovered as process `$GOCACHE`, else the `go env -w`
  file, else `%LocalAppData%\go-build`. A configured GOCACHE outside requested
  roots is skipped rather than falling back. npm, pip, and uv
  cache candidates still use Unix-oriented home paths such as `.npm\_cacache`,
  `.cache\pip`, and `.cache\uv`; do not rely on them to find normal Windows
  cache installations.
- Real Windows vendor-store layouts and migrations for Codex, Claude, Cursor,
  Windsurf, Gradle, and Cargo have not been broadly audited. Coverage of a
  fixture or a conventional home-relative dot directory is not a promise that
  every installed vendor layout is discovered.
- Registry-based, roaming-AppData, LocalAppData, mapped-drive, network-share,
  junction, and other reparse-point discovery layouts are not advertised as
  supported adapters. Safety checks are expected to fail closed when identity
  or containment cannot be established.
- Xcode cache discovery is intentionally excluded on Windows; it is enabled
  only on macOS.

Always run `aibris.exe scan --json` and `aibris.exe clean --dry-run` and inspect
the reported absolute paths before authorizing cleanup.
