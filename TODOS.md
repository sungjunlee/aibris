# TODOs

## Command-backed cache cleanup

**What:** Prefer official cache maintenance commands for caches that provide
them, such as `uv cache prune`, `go clean -cache`, and npm cache commands.

**Why:** Direct directory deletion works for simple reclaiming, but package
managers often maintain metadata and locking assumptions. Using the owning tool
is safer and easier to explain to users.

**Pros:** Lower risk for tool-owned caches, clearer user trust story, and a
better path toward AI-guided cleanup recommendations.

**Cons:** Command cleanup has different semantics from path deletion, can affect
more than the scanned item, may hang or fail if the tool is missing, and needs
careful dry-run behavior.

**Context:** The scan/cleanup improvement plan intentionally keeps cache
execution semantics unchanged so the first PR can focus on `$HOME` scan coverage
and active worktree protection. A follow-up should design command execution
with argv-only commands, context cancellation, clear fallback rules, and tests
for missing tools and failed commands.

**Depends on / blocked by:** Land the scan roots and worktree safety policy
first, so the cleanup execution change can be reviewed separately.
