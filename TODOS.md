# TODOs

## Active

No active TODOs.

## Completed

### Reuse recent scan results for faster clean

Implemented from GitHub issue #35. `scan` now writes a short-lived last-scan
snapshot, and `clean` reuses it when normalized roots, freshness, the explicit
cache revision (`schema_version`), and concrete provider membership match.
Provider membership does not detect behavior changes inside an unchanged
provider, which still require a cache revision bump. Each target is bound to
filesystem identity and type evidence, current modification time is refreshed
before selection, and identity, type, modification time, and age are revalidated
at the mutation boundary. Missing, replaced, symlink, reparse-point, or
unverifiable targets fail closed.

### Improve `clean` progress and target presentation

Implemented from GitHub issue #34. `clean` now shows scan progress, candidate
summary, clear target columns, no `?` display for non-project debris, and
per-item start progress before slow deletes or cleanup commands.

The previous command-backed cache cleanup follow-up shipped in PR #27 with
argv-only commands, context cancellation, fallback rules, tests, and docs.
