# Project Context

## Architecture Decisions

- `aibris` should stay a conservative scanner/executor. Human or AI judgment
  happens outside the CLI through `scan --json`.
- Default cleanup must protect active worktrees. Deleting valid worktrees is
  too risky to hide behind age-only filtering.
- `$HOME` scan coverage is the product promise, but it must prune high-noise
  personal/system directories and reject roots outside `$HOME`.

## Known Follow-Ups

- No open GitHub issues remain after PR #30. Next work should start from fresh
  product dogfooding or release feedback.
