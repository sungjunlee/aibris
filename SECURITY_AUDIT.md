# aibris Security Audit

This document describes the security-relevant behavior of `aibris`. Because it
is a local cleanup tool, its primary risk is unintended local data loss.

## Executive Summary

`aibris` scans known AI-development debris locations and can permanently delete
matching directories or files. It uses conservative defaults:

- classic cleanup targets must be older than `168h` by default; guided
  worktree recommendations use independent activity and retention gates
- destructive operations reject paths outside the user's home directory
- cleanup is limited to validated home-scoped paths and Git worktree metadata
- AI logs and similar sensitive artifacts require `--risky`
- `--dry-run`, interactive mode, and confirmation prompts are available before
  deletion
- active worktrees require Git-aware evidence and removal; raw recursive
  deletion is not used as a fallback
- partial scans are labeled, exit non-zero, invalidate cleanup caches, and
  cannot authorize cleanup

When a path or category is ambiguous, the tool should skip or reject it rather
than broadening cleanup scope.

## Threat Surface

The highest-risk areas are:

- recursive deletion through `os.RemoveAll`
- incorrect path classification in adapters
- symlink or path-prefix mistakes in safety checks
- overly broad generic worktree discovery
- accidental deletion of useful AI logs, session history, or active worktrees
- release and installation integrity for distributed binaries

The CLI does not accept arbitrary cleanup paths from users. Targets come from
registered adapters and are filtered before deletion.

## Destructive Operation Boundaries

Ordinary and orphaned targets go through the shared prepared cleanup executor
and `cleaner.ExecuteWithContext`, which check:

- the target path is absolute
- the target is under `$HOME`
- symlink-resolved home and target still keep the target under home when both
  paths can be resolved
- non-worktree paths contain a known-safe path component such as `.codex`,
  `.claude`, `.cursor`, `.cache`, `.npm`, `.gradle`, `.cargo`, `Caches`,
  `projects`, `.codeium`, or `node_modules`
- worktree paths carry scanner-validated active or orphaned Git metadata

Interactive cleanup uses the same prepared executor and target checks. Active
worktrees use a separate Git-aware executor: it captures cleanup-unit identity,
revalidates members and refs immediately before mutation, calls non-forced
`git worktree remove`, verifies the result, and never falls back to raw path
deletion after a Git failure.

## Path and Symlink Handling

`cleaner.IsSafePath` rejects relative paths and paths outside `$HOME`.

When possible, it resolves symlinks for both home and target and re-checks the
target boundary after resolution. If symlink resolution fails, the raw absolute
path must still be under `$HOME` and must still include a safe path component.

Known limitation: `os.RemoveAll` permanently removes the selected target. There
is no Trash or undo flow.

## Risky Categories

`ai-logs` and any unknown future category are risky by default. They are excluded
from cleanup unless the user passes `--risky`.

Risky examples include:

- AI session archives
- command audit logs
- file history
- Cursor and Windsurf project/session logs

These may contain useful debugging history or sensitive prompts, so a cleanup
miss is safer than accidental deletion.

## Dry-Run and Confirmation Controls

Safety controls before deletion:

- `aibris clean --dry-run` prints targets without deleting
- `aibris clean` asks for a final confirmation by default
- `aibris clean --interactive` asks for each item
- `aibris clean --force` is the only normal way to skip final confirmation
- `--age` must be positive
- `--age` below one hour prints a warning
- unknown category and tool selectors fail before scanning
- any execution failure is reflected in the receipt and process exit status

Default guided review adds stricter active-worktree controls: recent activity,
dirty state, current-directory membership, unreadable evidence, and unsafe
detached commits hard-lock a cleanup unit. `--force` skips only final
confirmation and cannot unlock a row or become Git's force option. After guided
review, remaining classic categories stay visible; overlapping targets are
normalized before dry-run output.

The AI-guided workflow in `skills/aibris/SKILL.md` is stricter than the raw CLI:
it requires dry-run first, user review, and then a second approval before real
cleanup.

## Release Integrity

Repository release controls include:

- GitHub Actions CI on push and pull requests
- `go test -race -count=1 -cover ./...`
- `go vet ./...`
- Dependabot for Go modules and GitHub Actions
- GoReleaser builds for Linux, macOS, and Windows
- GoReleaser checksum generation
- `install.sh` verifies downloaded release archives against `checksums.txt`

Future hardening should add artifact attestations and documented Homebrew tap
verification once that distribution path is published.

## Testing Coverage

Security-relevant behavior is covered by focused Go tests for:

- cleaner filtering
- safe path rejection
- symlink-aware path checks
- adapter discovery
- worktree health detection
- scanner context cancellation
- command-level dry-run and forced cleanup flows
- compiled-process stdout, stderr, prompt, signal, and exit-status contracts

Release readiness requires:

```bash
go test ./...
go build ./...
go vet ./...
```

## Known Limitations

- Deletion is permanent after confirmation; there is no restore command.
- Size estimation can be slow for very large dependency or cache trees.
- Worktree health is visible and active worktrees are protected by default, but
  there is no standalone `--status` selector.
- `$HOME` discovery is intentionally bounded and prunes noisy/system
  directories; explicitly excluded or unusually deep layouts may be missed.
- Homebrew installation is documented as pending until the tap is published.
- Release archives have checksums but do not yet publish an SBOM or signed
  provenance.
- Windows artifacts are built, but the full Windows behavior and support
  contract has not yet been established.
- The JSON top-level `worktrees` field contains all debris items for backward
  compatibility, not only worktrees.
