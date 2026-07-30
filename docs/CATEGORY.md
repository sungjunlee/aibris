# aibris Category Reference

`aibris` groups debris by category so users and agents can target one kind of
AI-workflow artifact without broad filesystem cleanup.

## Categories

| Category | Default clean | Risk | Description |
|----------|---------------|------|-------------|
| `worktree` | classic: orphaned only; guided Codex: evidence-based | low | Temporary Git worktrees discovered under `$HOME` by worktree directory conventions and validated `.git` metadata. Classic filters exclude active worktrees unless `--include-active-worktrees` is set; guided Codex review may recommend safe linked units. |
| `node_modules` | yes | medium | Project dependency folders under `$HOME` scan roots. They can be recreated with package managers. |
| `build-cache` | yes | medium | Go, Xcode, Gradle, npm, and Cargo caches. They are usually safe but may slow the next build. |
| `other-cache` | yes | low | pip and uv package caches. |
| `agent-state` | orphaned only | low | Claude and Cursor project-store entries classified from recorded working directories. Orphaned entries have no age gate; `live` and `undetermined` entries are always protected. |
| `ai-logs` | no | high | AI tool logs, archived sessions, file history, and similar records. Requires `--risky`. |

Unknown or future categories should stay risky until they have explicit safety
rules.

## Store-Nature Planning Taxonomy (Issue #142)

installed/regenerable/protected are planning taxonomy only, not `types.Category`, agent-state `classification`, a JSON field, or a current CLI selector.
The taxonomy records what a future provider may do; it does not make any of
these stores discoverable, selectable, or eligible today. When shallow metadata
cannot settle a store's nature, the decision fails closed to protected content.

| Store | Bounded evidence | Store-nature decision | Current and future consequence |
| --- | --- | --- | --- |
| `~/.codex/packages` | `standalone` has an installer lock, versioned architecture releases, and a `current` symlink to the active release. | Installed content | Excluded from providers, inventory, and every cleanup surface. No provider is planned. |
| `~/.codex/computer-use` | The directory contains the Codex Computer Use application bundle with bundle identity `com.openai.sky.CUAService`. | Installed content | Excluded from providers, inventory, and every cleanup surface. No provider is planned. |
| `~/.codex/tmp` | In the 2026-07-31 observation, the literal `~/.codex/tmp/path/` directory contained direct `codex-arg*` directories with paired `applypatch` and `apply_patch` shims; no upstream contract established that `path/` is a stable name. | Regenerable residue | Currently undiscovered, unselectable, and ineligible. A future L2 may consider only direct children of the tmp root, and only after each child passes the ownership and active-use/TOCTOU contract below. A basename such as `path/` is evidence, not identity or an allowlist; L2 must never delete the whole tmp root. |
| `~/.codex/generated_images` | ID directories contain generated PNG artifacts, which are user artifacts rather than a cache reconstruction input. | Protected content | Must not be default-clean or become deletable through `--risky` alone. It may be considered for explicit retention selection only after the #139 L1 semantics merge. |
| `~/.codex/sqlite` | Database filenames and schema names cover goals, threads, jobs, history snapshots, memories, logs, and state; live databases also have sidecar family members. | Protected content | Must not be default-clean or become deletable through `--risky` alone. A future provider is inventory-only unless it satisfies the fail-closed quiescence, family-registry, and atomic-manifest contract below. |
| `~/.cursor/ai-tracking` | `ai-code-tracking.db` schema names cover tracked-file content, conversation summaries, scored commits, deleted files, and tracking state. | Protected content | Must not be default-clean or become deletable through `--risky` alone. A future provider is inventory-only unless it satisfies the fail-closed quiescence, family-registry, and atomic-manifest contract below. |

This freezes the downstream split without defining the protected-content
runtime model reserved for #139:

- L2 may add only direct child units of `~/.codex/tmp`. The observed `path/`
  child is one example, not a stable selector. Its `codex-arg*` descendants and
  shim entries are evidence inside that unit, not independently selectable or
  deletable units. L2 must enumerate every direct child, evaluate it by the
  versioned safety contract below, and surface an unsupported child as
  protected and ineligible rather than silently skipping it. L2 must never
  delete `~/.codex/tmp` itself.
- L3 starts only after #139 L1 has merged. Generated images then follow that
  explicit retention-selection contract; Codex SQLite and Cursor AI tracking
  remain inventory-only absent the separate quiescence and atomic-family
  contract below.
- Installed content receives no provider. Uncertainty never widens cleanup
  eligibility.

### L2 tmp ownership and race-safety contract

Before a tmp provider can be registered, each supported Codex release/channel
and layout must have a versioned recognizer, fixtures, and these fail-closed
proofs:

- **Ownership:** the candidate resolves to one canonical, non-symlink direct
  child of the canonical tmp root, and producer-issued identity evidence or a
  documented upstream layout tied to the detected Codex version accounts for
  the child and every descendant. A basename, age, paired shim names, process
  name, or current absence of a writer is not ownership proof. An unknown
  version, entry type, link target, or descendant keeps the whole child
  protected.
- **Active use:** before enumeration, L2 acquires a producer-cooperative
  exclusive lock, lease, or pause handshake with an observable ownership or
  fencing token. Every writer must consult it, so none can start or mutate the
  unit until deletion finishes. The writer registry must cover the Codex
  application/CLI, apply-patch launchers and callers, and agent supervisors or
  background helpers. Process-name checks, `lsof`, `/proc`, and open-handle
  snapshots are advisory evidence only; if any writer is unknown or does not
  participate in the same exclusion protocol, the unit is ineligible.
- **TOCTOU:** while exclusion is held, L2 records the canonical path, file
  identities, complete member set, entry types, link targets, sizes, and
  modification times. It re-enumerates and compares that snapshot immediately
  before mutation, deletes only while exclusion remains held, and aborts the
  whole unit on lock loss or any mismatch. No partial deletion or byte credit is
  allowed.
- **Tests:** fixtures must cover every supported version/layout plus unknown
  direct children. Platform tests must race creation, mutation, rename, and
  exclusion loss from each writer class and prove that every race leaves the
  unit intact and ineligible.

### L3 protected-store inventory contract

The family definition is deliberately open and registry-driven, not an
exhaustive suffix list. Each supported store format/version must have a
versioned family registry that scans the bounded store directory and assigns
every entry to one primary database family or explicitly classifies it as
unrelated. The registry starts with the observed `-wal`, `-shm`, `-journal`,
`.wal`, `.shm`, `.journal`, `.backup`, and `.bak` conventions. A newly observed
store-specific journal, backup, or sidecar convention requires a registry and
fixture update before inventory resumes. Any entry the registry cannot assign
or prove unrelated makes the affected store incomplete, protected, and
non-inventoriable; it is never ignored.

A complete database family is one primary database plus every entry assigned
to it by that registry. One atomic manifest is one immutable record for the
complete family, listing each member's canonical path, file identity, size, and
modification time. Process quiescence means a producer-cooperative exclusive
lock, lease, shutdown, or pause protocol with an observable ownership or
fencing token prevents every registered database owner and background writer
from opening, creating, renaming, or modifying any store member. Each supported
store/version must register all such writers, including its application,
database connections, indexing or sync workers, and agent or supervisor
helpers. Exclusion is acquired before the first directory enumeration, its
token is revalidated before and after each publication step, and it is held
without interruption until after publication and directory durability are
confirmed. An unknown or non-participating writer means quiescence is
unprovable. `lsof`, `/proc`, process-name checks, and point-in-time open-handle
enumeration do not prove quiescence on any platform.

While quiescence is held, L3 captures the family, writes and syncs a temporary
manifest, re-enumerates and compares the family and exclusion token, atomically
replaces the same-directory destination, makes the directory entry durable,
and verifies the token again before releasing exclusion. The provider may emit
or accept that manifest as inventory only after every step succeeds; a file
left by an interrupted or previous attempt is never inventory evidence. Lock
loss or other mid-publish violation, a non-participating writer, membership or
metadata drift, sync or replace failure, or a platform without the required
atomic-replace and directory-durability primitives aborts the operation,
removes attempt files where exclusion still permits it, and emits no inventory.
Platform and fault-injection tests must cover writer races, new sidecars, lock
loss before and after replacement, and every publication failure. Until those
proofs exist, Codex SQLite and Cursor AI tracking remain protected and
inventory-only in planning, with no L3 provider.

## Agent-State Classification

The `classification` field applies to `agent-state` entries and is omitted for
other categories:

| Classification | Meaning | Cleanup eligibility |
|----------------|---------|---------------------|
| `live` | At least one recorded working directory still exists. | Protected |
| `orphaned` | Every usable recorded working directory is proven absent. | Eligible without an age gate |
| `undetermined` | Recorded working-directory evidence is missing, unreadable, ambiguous, or otherwise inconclusive. | Protected |

Classification takes precedence over age because an absent recorded working
directory proves the associated work is gone and resume is already impossible.
Directory modification time cannot strengthen or weaken that proof.

Classification also applies across category boundaries. A `live` or
`undetermined` agent-state row that contains, is contained by, or exactly
aliases another cleanup target shields that target's complete physical
component. Conversely, a generic outer owner containing one or more orphaned
agent-state rows inherits every child revalidation obligation. The outer owner
is counted once; nested agent-state rows remain zero-byte evidence with their
canonical path, tool, classification, policy reason, overlap reason, and final
revalidation outcome.

## Tool Mapping

| Tool | Category | Notes |
|------|----------|-------|
| `codex` | `worktree` | Path-derived source `.codex`. |
| `claude` | `worktree` | Path-derived source `.claude`. |
| `claude` | `agent-state` | `~/.claude/projects` entries classified from session `cwd` metadata. |
| `cursor` | `agent-state` | `~/.cursor/projects` entries classified from all distinct usable `workspacePath=` values in `worker.log`; any live workspace wins. |
| `unknown` | `worktree` | Generic worktree convention discovery for future or local tools; inspect `source` for the path-derived owner. |
| `node_modules` | `node_modules` | Dependency directories under scan roots, defaulting to `$HOME`. |
| `build-cache` | `build-cache` | Language and platform build caches. |
| `pip-cache` | `other-cache` | Python package caches. |
| `windsurf` | `ai-logs` | Windsurf logs and cache-style AI artifacts. |
| `ai-logs` | `ai-logs` | Codex and Claude log/history locations. |

Cursor project-store migration is explicit at the category boundary:
`--category ai-logs` no longer matches `~/.cursor/projects`, while
`--category agent-state` now does. An orphaned Cursor entry is eligible for
default cleanup immediately, without an age gate. A `live` or `undetermined`
entry is protected even with `--risky --force`. The `ai-logs` category is not
empty: it still includes the Windsurf adapter and the generic AI log provider.

## Filter Semantics

`aibris clean` combines filters with AND semantics:

```bash
aibris clean --category worktree --tool codex --age 7d --dry-run
```

This explicit selector uses classic cleanup and means:

- category must be `worktree`
- tool must be `codex`
- item must be older than 7 days
- risky categories are excluded unless `--risky` is set
- active worktrees are excluded unless `--include-active-worktrees` is set

Empty `--category` means all categories allowed by `--risky`. Empty `--tool`
means all tools.

With no classic selector, plain `clean` uses guided Codex review when validated
active pressure reaches 256 MB or three physical units. Guided cleanup groups
members by physical target, groups retention by canonical Git common-dir, and
classifies rows as recommended, reviewable, or locked. Its independent defaults
are a 6-hour recent-activity hard lock, three retained units per repository, a
3-day minimum idle age, and a 256 MB recommendation threshold. Missing upstream
does not lock a row; dirty state, unavailable evidence, and an unreferenced
detached HEAD do.

Scan roots default to `$HOME`. Use repeatable `--root` flags to narrow scope:

```bash
aibris scan --root ~/.codex --json
aibris clean --root ~/path/to/project --category node_modules --dry-run
```

Roots must resolve under `$HOME`; `/`, `/tmp`, and symlink escapes are rejected.

Supported command-backed cleanup:

| Item | Command |
|------|---------|
| `go-build` | `go clean -cache` |
| `npm` | `npm cache clean --force` |
| `uv` | `uv cache prune` |

If the command is missing, aibris falls back to safe path removal. If the
command runs and fails, aibris reports the error and does not remove the path.

Age values accept human units such as `7d`, `2w`, `1mo`, and `1y`. Use `mo` for
months; bare `m` keeps the Go duration meaning of minutes.

## Agent Integration Pattern

After scanning and receiving approval, choose one of these distinct branches.

### Selector-preserving cleanup

For a scoped cleanup, the preview and execution commands must be identical
except that execution removes `--dry-run`. Preserve every user-approved
`--category`, `--tool`, repeatable `--root`, and `--age` value, plus applicable
routing and safety flags such as `--guide`, `--no-guide`, `--risky`,
`--include-active-worktrees`, `--interactive`, and `--force`. Never follow a
scoped preview with plain `aibris clean`.

```bash
aibris scan --json
aibris clean --no-guide --root ~/path/to/project --category worktree --tool codex --age 7d --include-active-worktrees --dry-run
aibris clean --no-guide --root ~/path/to/project --category worktree --tool codex --age 7d --include-active-worktrees
```

### No-selector guided Codex cleanup

Use the plain-command pair only when the user approved an unscoped guided
Codex review and did not approve any CLI selector or safety flag:

```bash
aibris scan --json
aibris clean --dry-run
aibris clean
```

Agents should summarize worktrees by `source`, `project`, and `status`, ask the
user what to remove, use guided evidence for active Codex worktrees, run a
dry-run first, and only execute cleanup after a second explicit confirmation.
Classic active cleanup still needs `--include-active-worktrees`; guided active
cleanup needs an explicitly accepted recommended or reviewable row. Active
members are removed through non-forced Git worktree semantics with branch-ref
and parent-metadata verification.
