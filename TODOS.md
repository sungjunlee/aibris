# TODOs

## Active

### Reuse recent scan results for faster clean

**What:** Let `aibris clean` reuse a fresh compatible `aibris scan` snapshot
instead of immediately rescanning the same roots.

**Why:** Dogfood showed the back-to-back `scan` then `clean` workflow repeats
work. The progress fixes make this visible, but a fresh scan cache would make
the common path faster.

**Start here:** GitHub issue #35.

**Must handle:** stale paths, root mismatch, CLI/schema version mismatch, and
safe-path checks before deletion.

**Depends on:** The no-cache `clean` progress and target presentation fix.

## Completed

### Improve `clean` progress and target presentation

Implemented from GitHub issue #34. `clean` now shows scan progress, candidate
summary, clear target columns, no `?` display for non-project debris, and
per-item start progress before slow deletes or cleanup commands.

The previous command-backed cache cleanup follow-up shipped in PR #27 with
argv-only commands, context cancellation, fallback rules, tests, and docs.
