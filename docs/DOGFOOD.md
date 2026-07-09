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

## 2026-07-07 - Guided Codex Cleanup Dry Run

Command:

```bash
printf '\n' | GOCACHE=/private/tmp/aibris-gocache-55 go run . clean --guide --dry-run --root /Users/sjlee/.codex
```

Output:

```text
clean
  roots  ~/.codex

  scanning node_modules
  scanning build-cache 
  scanning pip-cache   
  scanning cursor      
  scanning ai-logs     
  scanning windsurf    
  scanning codex       
  found    cursor         0 items   0 B

  found    windsurf       0 items   0 B

  found    pip-cache      0 items   0 B

  found    ai-logs        2 items   1.4 GB

  found    build-cache    0 items   0 B

  found    node_modules  24 items   22.2 GB

  found    codex         40 items   41.4 GB

guided codex worktree cleanup

scan
  source     live
  activity   indexed

summary
  selected   3 items   3.1 GB
  protected  36 items   37.3 GB

selected for cleanup
  [x]  1    1.0 GB  baby_ops-issue-184-export active 11d         zero matching Codex sessions
  [x]  2    1.0 GB  baby_ops-issue-185-filters active 11d         zero matching Codex sessions
  [x]  3    1.0 GB  baby_ops-issue-186-empty-states active 11d         zero matching Codex sessions

protected
  [ ]  4    7.3 GB  07af                     active 14d         dirty files, upstream comparison unavailable
  [ ]  5    5.2 GB  d9f73c9f-9ace-43aa-88d5-853c79dcf8d1 active 12d         dirty files, upstream comparison unavailable
  [ ]  6    4.6 GB  baby_ops-dogfood-install active 11d         dirty files, upstream comparison unavailable
  [ ]  7    2.3 GB  3247                     active 2d          upstream comparison unavailable
  [ ]  8    1.6 GB  beopjalal-actions-runner-labels active 13d         upstream comparison unavailable
  [ ]  9    1.1 GB  baby_ops-uiux-integration-pass active 10d         dirty files, upstream comparison unavailable
  [ ] 10    1.1 GB  5e0a                     active 6d          upstream comparison unavailable
  [ ] 11    1.1 GB  baby_ops-v3-visual-smoke active 7d          upstream comparison unavailable
  [ ] 12    1.0 GB  cdf9                     active 11d         upstream comparison unavailable
  [ ] 13    1.0 GB  baby_ops-correction-sheet-visual-qa active 13d         upstream comparison unavailable
  [ ] 14    1.0 GB  baby_ops-correction-type-expansion active 13d         upstream comparison unavailable
  [ ] 15    1.0 GB  baby_ops-delete-undo     active 13d         upstream comparison unavailable
  [ ] 16    1.0 GB  baby_ops-correction-trust-final active 13d         upstream comparison unavailable
  [ ] 17    1.0 GB  baby_ops-manual-fallback active 13d         upstream comparison unavailable
  [ ] 18    1.0 GB  baby_ops-correction-a11y active 13d         upstream comparison unavailable
  [ ] 19    1.0 GB  baby_ops-feeding-amount  active 13d         upstream comparison unavailable
  [ ] 20    1.0 GB  baby_ops-source-detail   active 13d         upstream comparison unavailable
  [ ] 21    1.0 GB  baby_ops-mvp-0-5-design-spike active 13d         upstream comparison unavailable
  [ ] 22    1.0 GB  6f885429-e3e8-43db-97a6-074fd8f16c3d active 15d         git status unavailable
  [ ] 23  506.4 MB  caab                     active 2d          upstream comparison unavailable
  ... 16 more protected   1.1 GB

Enter numbers to toggle, Enter to preview, q to abort: clean plan
  mode     dry-run
  targets  3 items   3.1 GB

targets
      size  category      name         project            age/status     action       reason
    1.0 GB  worktree      baby_ops-issue-184-export baby_ops-issue-184-export active 11d     remove-path  active worktree
    ~/.codex/worktrees/baby_ops-issue-184-export
    1.0 GB  worktree      baby_ops-issue-185-filters baby_ops-issue-185-filters active 11d     remove-path  active worktree
    ~/.codex/worktrees/baby_ops-issue-185-filters
    1.0 GB  worktree      baby_ops-issue-186-empty-states baby_ops-issue-186-empty-states active 11d     remove-path  active worktree
    ~/.codex/worktrees/baby_ops-issue-186-empty-states

[DRY-RUN] Preview complete.
[DRY-RUN] No files were removed.
```

Notes:

- The guide default-selected 3 low-risk Codex worktrees while protecting 36
  active worktrees with dirty, unavailable upstream, newest, or other safety
  reasons.
- Pressing Enter handed the selected rows to the normal dry-run clean plan.
- `--dry-run` completed without deleting files.

## 2026-07-09 - Default Guided Clean Dry Run

Command:

```bash
printf '\n' | GOCACHE=/private/tmp/aibris-gocache-66 go run . clean --dry-run --root /Users/sjlee/.codex
```

Observed result:

```text
clean
  roots  ~/.codex

guided codex worktree cleanup

scan
  source     live
  activity   indexed

summary
  selected   3 items   3.1 GB
  protected  36 items   30.8 GB

selected for cleanup
  [x]  1    1.0 GB  baby_ops-issue-184-export active 11d         zero matching Codex sessions
  [x]  2    1.0 GB  baby_ops-issue-185-filters active 11d         zero matching Codex sessions
  [x]  3    1.0 GB  baby_ops-issue-186-empty-states active 11d         zero matching Codex sessions

Enter numbers to toggle, Enter to preview, q to abort: clean plan
  mode     dry-run
  targets  3 items   3.1 GB

[DRY-RUN] Preview complete.
[DRY-RUN] No files were removed.
```

Notes:

- `clean --dry-run` entered guided Codex worktree cleanup without `--guide`
  because useful active Codex worktree recommendations existed.
- The reason was `active Codex worktrees are the largest cleanup decision`.
- `--root /Users/sjlee/.codex` narrowed scan scope but did not disable default
  guided routing.
- Dry-run rendered the selected 3-item clean plan and deleted nothing.
