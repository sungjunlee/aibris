# aibris Category Reference

`aibris` groups debris by category so users and agents can target one kind of
AI-workflow artifact without broad filesystem cleanup.

## Categories

| Category | Default clean | Risk | Description |
|----------|---------------|------|-------------|
| `worktree` | yes | low | Temporary Git worktrees created by Codex, Claude, relay-style workflows, or other tools. |
| `node_modules` | yes | medium | Project dependency folders under configured project roots. They can be recreated with package managers. |
| `build-cache` | yes | medium | Go, Xcode, Gradle, npm, and Cargo caches. They are usually safe but may slow the next build. |
| `other-cache` | yes | low | pip and uv package caches. |
| `ai-logs` | no | high | AI tool logs, archived sessions, file history, and similar records. Requires `--risky`. |

Unknown or future categories should stay risky until they have explicit safety
rules.

## Tool Mapping

| Tool | Category | Notes |
|------|----------|-------|
| `codex` | `worktree` | Known Codex worktree layout. |
| `claude` | `worktree` | Known Claude Code worktree layout. |
| `unknown` | `worktree` | Generic `worktree*` discovery for future or local tools. |
| `node_modules` | `node_modules` | Dependency directories under `~/projects`. |
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

This command means:

- category must be `worktree`
- tool must be `codex`
- item must be older than 7 days
- risky categories are excluded unless `--risky` is set

Empty `--category` means all categories allowed by `--risky`. Empty `--tool`
means all tools.

Age values accept human units such as `7d`, `2w`, `1mo`, and `1y`. Use `mo` for
months; bare `m` keeps the Go duration meaning of minutes.

## Agent Integration Pattern

The intended AI-guided cleanup loop is:

```bash
aibris scan --json
aibris clean --category <category> --tool <tool> --age <duration> --dry-run
aibris clean --category <category> --tool <tool> --age <duration>
```

Agents should summarize scan results, ask the user what to remove, run a dry-run
first, and only execute cleanup after a second explicit confirmation.
