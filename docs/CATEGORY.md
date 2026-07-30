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
| `~/.codex/tmp` | The literal `~/.codex/tmp/path/` directory contains direct `codex-arg*` directories with paired `applypatch` and `apply_patch` shims. | Regenerable residue | Currently undiscovered, unselectable, and ineligible. It is only a future safety-bounded default-clean candidate: for the observed layout, L2's only cleanup unit is the direct child `~/.codex/tmp/path/`; L2 must prove ownership and active-use/TOCTOU safety and must never delete the whole tmp root. |
| `~/.codex/generated_images` | ID directories contain generated PNG artifacts, which are user artifacts rather than a cache reconstruction input. | Protected content | Must not be default-clean or become deletable through `--risky` alone. It may be considered for explicit retention selection only after the #139 L1 semantics merge. |
| `~/.codex/sqlite` | Database filenames and schema names cover goals, threads, jobs, history snapshots, memories, logs, and state; live databases also have sidecar family members. | Protected content | Must not be default-clean or become deletable through `--risky` alone. A future provider is inventory-only unless a separate contract proves process quiescence and supplies one atomic manifest for every database/WAL/SHM family, using the complete family and manifest definitions below. |
| `~/.cursor/ai-tracking` | `ai-code-tracking.db` schema names cover tracked-file content, conversation summaries, scored commits, deleted files, and tracking state. | Protected content | Must not be default-clean or become deletable through `--risky` alone. A future provider is inventory-only unless a separate contract proves process quiescence and supplies one atomic manifest for every database/WAL/SHM family, using the complete family and manifest definitions below. |

This freezes the downstream split without defining the protected-content
runtime model reserved for #139:

- L2 may add only direct child units of `~/.codex/tmp`. For the observed layout,
  that means exactly the literal `~/.codex/tmp/path/` directory. Its
  `codex-arg*` grandchildren and their shim entries are evidence inside that
  unit, not independently selectable or deletable units. L2 must prove
  ownership plus active-use/TOCTOU safety for the entire `path/` unit and must
  never delete `~/.codex/tmp` itself. Any other root-level entry requires a new
  classification decision before L2 may select it.
- L3 starts only after #139 L1 has merged. Generated images then follow that
  explicit retention-selection contract; Codex SQLite and Cursor AI tracking
  remain inventory-only absent the separate quiescence and atomic-family
  contract below.
- Installed content receives no provider. Uncertainty never widens cleanup
  eligibility.

For that L3 contract, a database/WAL/SHM family means one primary database plus
every same-directory member derived from it, including `-wal`, `-shm`,
`-journal`, `.wal`, `.shm`, `.journal`, `.backup`, and `.bak` siblings. L3 must
enumerate any additional store-specific journal, backup, or sidecar convention
before inventory; an unassociated candidate makes the family incomplete and
protected. One atomic manifest means one immutable record for the complete
family, captured while process quiescence is continuously held. It lists each
member's canonical path, file identity, size, and modification time; it is
published all-or-nothing by a synced temporary write and same-directory rename,
with a parent-directory sync where the platform supports it. If membership or
recorded metadata changes before publication, L3 must abort and publish no
manifest.

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
