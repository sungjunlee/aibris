# Project Context

## Architecture Decisions

- `aibris` should stay a conservative scanner/executor. Human or AI judgment
  happens outside the CLI through `scan --json`.
- Default cleanup must protect active worktrees. Deleting valid worktrees is
  too risky to hide behind age-only filtering.
- `$HOME` scan coverage is the product promise, but it must prune high-noise
  personal/system directories and reject roots outside `$HOME`.

## Known Follow-Ups

- Sprint `Guided Codex Cleanup` tracks the active Codex worktree bloat follow-up
  from local dogfooding. GitHub milestone #3 owns epic #49 and issues #50-#55.
