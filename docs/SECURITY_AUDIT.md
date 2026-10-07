# aibris Security Audit

As of 2026-10-07, this is a current security summary for this checkout, not a
record of a particular released binary. The primary risk is unintended local
data loss. Canonical contracts live in [SPEC.md](SPEC.md) (cleanup safety),
[JSON_SCHEMA.md](JSON_SCHEMA.md) (plans and receipts), [INSTALL.md](INSTALL.md)
(release integrity and installation), and [WINDOWS.md](WINDOWS.md) (native
Windows support). Historical findings are labeled in the
[dogfood notes](DOGFOOD.md); release status is in [ROADMAP.md](ROADMAP.md).

## Executive Summary

`aibris` scans known AI-development debris locations and can permanently delete
matching directories or files. It uses conservative defaults:

- classic cleanup targets must be older than `168h` by default, except
  proof-classified orphaned agent state; guided worktree recommendations use
  independent activity and retention gates
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

Every removal, cleanup command, and Git worktree removal passes
`internal/safedelete`: the canonical target must be strictly inside `$HOME`,
outside protected locations and their ancestors, and outside primary Git
repositories and Git metadata. Only that package performs recursive deletion.
The detailed invariants are maintained in [AGENTS.md](../AGENTS.md) and the
[cleanup safety contract](SPEC.md#safety-requirements).

The shared prepared executor retains typed target, overlap, filesystem, and
Git evidence through review and execution. Scan/cache inventory is never
deletion authority. Executors re-derive evidence at the mutation boundary and
refuse drift or incomplete activity evidence; cancellation is checked after
the barrier and before mutation. Receipts preserve any already completed
reclamation when a later target fails or is cancelled.

Orphaned worktrees must still be wholly orphaned with unchanged members and
regular `.git` marker files. Restored Git metadata, changed markers, symlinked
markers, or newly active members refuse deletion rather than switching to an
active removal route. Active removal uses non-forced `git worktree remove`,
preserves refs, and never falls back to raw deletion after a Git failure.

Command recipes are re-derived from the live cache catalog, including tool,
category, canonical path, argv, and pinned cache environment. Recipe drift
refuses execution and path fallback. Homebrew cache cleanup removes only the
verified cache path through the deletion gate. Receipt refusal codes and
fallback semantics are maintained in [JSON_SCHEMA.md](JSON_SCHEMA.md#clean-execution-receipt).

## Path and Symlink Handling

Containment and protected-path checks resolve symlinks and fail closed when
identity or containment cannot be established. Cached symlink or Windows
reparse-point targets cannot provide reusable cleanup identity. See
[SPEC.md](SPEC.md) for cache evidence and [WINDOWS.md](WINDOWS.md) for native
platform boundaries. Deletion is permanent; there is no Trash or undo flow.

## Risky Categories

Claude, Cursor, and Grok project/session stores are `agent-state`; Cursor
project state is not `ai-logs`. Proven orphaned entries ignore classic `--age`,
but default selection waits for `--agent-state-grace` (default `24h`). `0`
disables that floor; negative values are rejected. The floor uses the newest
in-tree activity and is rechecked immediately before mutation when enabled.
`live` and `undetermined` entries are always protected. The classification is
proved from working-directory metadata recorded in each entry rather than
inferred from the entry name or path.

`ai-logs` and any unknown future category are risky by default. They are excluded
from cleanup unless the user passes `--risky`.

Risky examples include:

- generic `ai-logs` provider artifacts, including AI session archives, command
  audit logs, and file history
- Windsurf project/session logs

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

Release controls implemented in this checkout include:

- Required CI verification of the tag event's exact SHA before a draft release
  can be created, covering both Go modules and the native Windows safety job.
- An SPDX SBOM per archive and GitHub build provenance attestations. The order
  is draft, attestation, public release, Homebrew tap update, then macOS pour
  verification. Failure or cancellation blocks dependent steps.
- `checksums.txt` verification by the Unix `install.sh` and native PowerShell
  `install.ps1` installers. Both stage a replacement beside the destination;
  failed download, verification, staging, or replacement preserves the existing
  binary. The Windows installer also refuses a locked binary.
- The third-party Homebrew tap `sungjunlee/tap/aibris`
  (https://github.com/sungjunlee/homebrew-tap). Homebrew item-trusts the formula;
  formula hashes and `checksums.txt` have the same publisher (TOFU), not a
  second signer or `homebrew/core` review.

Artifact verification commands and installer trust boundaries are maintained
in [INSTALL.md](INSTALL.md); native installer and platform assurance are in
[WINDOWS.md](WINDOWS.md). These repository contracts do not establish that a
new release was published or a native platform was run during a local audit.

## Testing Coverage

Focused tests cover deletion gates, containment, identity/Git/catalog drift,
agent-state classification, incomplete activity, cancellation, dry-run,
receipts, installer failures, and the release dependency graph. Local release
tests use stub publication and download fixtures. Onboarding prerequisites,
root/nested-module checks, and reporting of unrun platforms are maintained in
[CONTRIBUTING.md](../CONTRIBUTING.md).

## Known Limitations

- Deletion is permanent after confirmation; there is no restore command.
- Size estimation can be slow for very large dependency or cache trees.
- Worktree health is visible and active worktrees are protected by default, but
  there is no standalone `--status` selector.
- `$HOME` discovery is intentionally bounded and prunes noisy/system
  directories; explicitly excluded or unusually deep layouts may be missed.
- Homebrew installation uses the third-party tap `sungjunlee/tap`; Homebrew
  item-trusts the formula, and checksums are same-publisher TOFU, not
  `homebrew/core` review.
- Windows artifacts remain experimental; native amd64 CI and the PowerShell
  installer do not establish native arm64 or every vendor-store layout. See
  [WINDOWS.md](WINDOWS.md) for tested and unaudited boundaries.
- The JSON top-level `worktrees` field contains all debris items for backward
  compatibility, not only worktrees.
