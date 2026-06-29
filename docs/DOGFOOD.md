# Dogfood Notes

These notes capture real local runs used to validate release behavior. Paths
and counts are machine-specific; the point is to preserve observed CLI shape
and safety behavior.

## 2026-06-06 - Scan UX After PR #30

Command:

```bash
go run . scan --root /Users/sjlee/workspace/active/harness-stack
```

Output:

```text
scan
  roots  ~/workspace/active/harness-stack

  scanning node_modules
  scanning build-cache
  scanning pip-cache
  scanning cursor
  scanning ai-logs
  scanning windsurf
  scanning codex
  found    build-cache    0 items   0 B
  found    pip-cache      0 items   0 B
  found    windsurf       0 items   0 B
  found    cursor         0 items   0 B
  found    ai-logs        0 items   0 B
  found    codex          1 items   1.4 MB
  found    node_modules   0 items   0 B

summary
  found       1 item
  found size  1.4 MB
  default clean 0 B
  protected   1.4 MB active worktrees; use --include-active-worktrees after review

by category
  worktree        1   1.4 MB

largest
    1.4 MB  worktree      elegant-ardinghelli dev-relay          active 49d

next
  aibris clean --dry-run
  aibris scan --json
```

Notes:

- Non-interactive output stayed log-friendly with plain `scanning` and `found`
  lines.
- The active worktree appeared in scan output but remains protected from
  default cleanup.

## 2026-06-29 - Clean Audit Output

Command:

```bash
go run . clean --root /Users/sjlee/workspace/active/harness-stack/aibris --dry-run
```

Output:

```text
clean
  roots  ~/workspace/active/harness-stack/aibris

  scanning node_modules
  scanning build-cache
  scanning pip-cache
  scanning cursor
  scanning ai-logs
  scanning windsurf
  scanning codex
  found    pip-cache      0 items   0 B

  found    cursor         0 items   0 B

  found    ai-logs        0 items   0 B

  found    windsurf       0 items   0 B

  found    codex          0 items   0 B

  found    build-cache    0 items   0 B

  found    node_modules   0 items   0 B

  policy  age>7d, risky=false, active-worktrees=protected
  scan    live

scan summary
  scanned    7 sources   0 items   0 B
  eligible   0 items   0 B
  protected/skipped 0 items   0 B

  matched  0 candidates   0 B

No items to clean.
```
