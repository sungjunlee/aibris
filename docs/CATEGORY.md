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
| `ai-logs` | no | high | AI tool logs, archived sessions, file history, and similar records. Requires `--risky`. |

Unknown or future categories should stay risky until they have explicit safety
rules.

## Tool Mapping

| Tool | Category | Notes |
|------|----------|-------|
| `codex` | `worktree` | Path-derived source `.codex`. |
| `claude` | `worktree` | Path-derived source `.claude`. |
| `unknown` | `worktree` | Generic worktree convention discovery for future or local tools; inspect `source` for the path-derived owner. |
| `node_modules` | `node_modules` | Dependency directories under scan roots, defaulting to `$HOME`. |
| `build-cache` | `build-cache` | Language and platform build caches. |
| `pip-cache` | `other-cache` | Python package caches. |
| `cursor` | `ai-logs` | Cursor project/session logs. |
| `windsurf` | `ai-logs` | Windsurf logs and cache-style AI artifacts. |
| `ai-logs` | `ai-logs` | Codex and Claude log/history locations. |

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
