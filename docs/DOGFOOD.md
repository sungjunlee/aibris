# Evidence-Based Reclamation Dogfood

These notes record the sanitized evidence used for issue #90. The real `$HOME`
exercise was limited to scan, Git inspection, and dry-run planning. The only
deletion was a disposable branch-backed linked worktree under a temporary
`HOME`; all temporary repositories and worktrees were removed afterward.

## Preserved 2026-07-10 Before Baseline

The v0.7.0 planner treated missing upstream comparison as hard safety. Its
preserved 39-unit, 33.9 GB baseline was:

| Class | Units | Size |
| --- | ---: | ---: |
| Recommended | 3 | 3.1 GB |
| Reviewable | 2 | 0.1 GB |
| Locked | 34 | 30.7 GB |
| Total | 39 | 33.9 GB |

The locked-reason distribution was:

| Reason | Units | Size |
| --- | ---: | ---: |
| Upstream comparison unavailable | 26 | 17.2 GB |
| Dirty files and upstream comparison unavailable | 6 | 11.0 GB |
| Git status unavailable | 2 | 2.5 GB |

Canonical Git common-dir inspection reduced 26 path-derived project labels to
six repositories plus unresolved units. Twelve detached HEADs accounted for
6.0 GB; all twelve were reachable from named refs. Two physical targets had two
nested Git members each, but the old single-member inspector reported them as
unavailable.

## 2026-07-13 Live Read-Only Run

Commands:

```bash
aibris scan --json
printf '\n' | aibris clean --dry-run
printf '\n' | aibris clean --dry-run --age 14d
```

The full scan found 140 items / 20.5 GB. Worktrees accounted for 43 items /
14.7 GB across Codex, Claude, and other convention owners. Guided cleanup
considered the 19 active `.codex` cleanup units / 6.8 GB.

This run happened inside a read-only `$HOME` sandbox, so Codex activity was
unavailable. The planner correctly failed closed instead of substituting weak
evidence or fabricating a recommendation:

```text
guided codex worktree cleanup
  policy     idle>3d, recent<6h locked, keep=3/repo, min-size=256.0 MB

scan
  source     live
  activity   unavailable

summary
  selected   0 items   0 B
  projected  0 B
  protected  19 items   6.8 GB
```

Changing `--age` to `14d` changed only the header to `idle>14d`; the 6-hour
lock and recent-three setting stayed fixed. All rows remained locked because
activity evidence was unavailable, not because age replanning silently changed
another safety input.

### Sanitized Git Evidence

Read-only member inspection produced this distribution. Repository names and
paths are replaced by stable aliases.

| Evidence | Distribution |
| --- | --- |
| Canonical repositories | 5 groups containing 8, 4, 4, 2, and 1 units |
| Unit member count | 19 one-member units; no live multi-member unit remained |
| Worktree state | 11 clean, 8 dirty or untracked |
| HEAD state | 8 attached, 11 detached |
| Detached reachability | 11 reachable from named refs, 0 unreferenced |
| Attached upstream | 4 configured, 4 missing or gone |

The corresponding visible reason counts were:

| Reason or metadata | Rows | Policy effect |
| --- | ---: | --- |
| Activity evidence unavailable | 19 | Hard lock |
| Dirty or untracked files | 8 | Additional hard lock |
| Detached HEAD unreferenced | 0 | No live occurrence; hard lock is fixture-tested |
| Upstream missing or gone | 4 | Explanation only; zero rows locked solely for upstream |

This live state had drifted below the accepted 10 GB recommendation bar because
required activity evidence was unavailable. The observed recommendation remains
truthfully 0 B.

## Deterministic Accepted-Baseline Fixture

`TestEvidenceBasedReclamationBaseline` preserves the accepted 39-unit /
33.9 GB shape without depending on mutable local state:

| Decision | Units | Size |
| --- | ---: | ---: |
| Hard locked | 8 | 13.5 GB |
| Recent-three retained | 11 | 6.7 GB |
| Age or size hold | 7 | 0.2 GB |
| Recommended | 13 | 13.5 GB |
| Total | 39 | 33.9 GB |

The fixture includes six dirty units, one unreferenced detached HEAD, one unit
with unavailable Git evidence, an attached branch with no upstream, a detached
HEAD reachable from a named remote ref, and a safe two-member cleanup unit. It
asserts that:

- all dirty, unreferenced-detached, and unavailable-evidence units remain
  locked;
- missing upstream does not lock an otherwise safe unit;
- the two-member unit is inspected and recommended as one physical target;
- canonical repository IDs, rather than display names, drive recent-three
  retention;
- recommended bytes are 13.5 GB, above the 10 GB acceptance threshold; and
- raising minimum idle age changes recommendations while leaving hard locks and
  recent-three retention unchanged.

Temporary-Git integration tests separately discover both nested `.git` members,
aggregate a dirty member into a unit lock, preflight every member before
mutation, and verify partial receipts do not claim freed bytes. This guards the
member-inspection seam rather than only feeding prebuilt members to the policy.

Run it with:

```bash
go test ./cmd -run TestEvidenceBasedReclamationBaseline -count=1
```

## Disposable Git-Aware Removal

A temporary repository was initialized with `main`, then a local-only branch
`preserve-me` was checked out as a linked worktree under
`<temp-home>/.codex/worktrees/disposable`. The worktree was made old enough for
the classic selector and previewed first:

```bash
HOME=<temp-home> aibris clean \
  --root <temp-home>/.codex \
  --category worktree \
  --include-active-worktrees \
  --age 1h \
  --dry-run
```

Sanitized preview:

```text
policy  age>1h, risky=false, active-worktrees=included
eligible   1 item   4.0 KB
matched    1 candidate   4.0 KB
[DRY-RUN] No files were removed.
```

The same command without `--dry-run` used `--force` only to make this automated
fixture non-interactive. The executor did not pass force to Git:

```text
removing worktree member 1/1: <temp-home>/.codex/worktrees/disposable ...
removed worktree member: <temp-home>/.codex/worktrees/disposable

worktree execution receipt
  unit      removed <temp-home>/.codex/worktrees/disposable
    member  removed <temp-home>/.codex/worktrees/disposable
    physical-removed true   freed 4.0 KB
```

Postconditions:

| Check | Before | After |
| --- | --- | --- |
| `refs/heads/preserve-me` | captured OID | same OID |
| Physical member path | exists | absent |
| Parent `git worktree list` | parent + disposable member | parent only |
| Unrelated `main` worktree | present | present |

This exercises the shipped preflight, non-forced `git worktree remove`, branch
verification, metadata verification, and receipt path. No real user worktree was
deleted.

## 2026-07-26 Agent State Store Coverage Audit

This is a read-only coverage audit of one real developer `$HOME`, run against a
build of `main`. It measures what fraction of *agent-produced* leftovers aibris
actually discovers. No deletion was performed. Sizes come from `du`; aibris
figures come from `aibris scan --json`. Project names are omitted.

### Method

```bash
go build -o <tmp>/aibris-head .
<tmp>/aibris-head scan                 # historical observation only: 19.2s wall clock, full $HOME
<tmp>/aibris-head scan --json
<tmp>/aibris-head scan --root ~/.codex --json
<tmp>/aibris-head clean --dry-run < /dev/null
```

Installed content — `~/.claude/skills`, `~/.claude/plugins`, `~/.cursor/extensions`,
`~/.codex/plugins` — is excluded from every total below. aibris correctly does
not classify installed content as debris, and that boundary must be preserved.

The `19.2s` result is a historical observation from this one 2026-07-26 run,
not a performance baseline. Filesystem-cache state alone later produced an
approximately 11–35 second spread on an unchanged binary. Neither `19.2s` nor
any other stored absolute real-home timing may be used as a regression target.

### Same-session paired scan delta protocol

Provider changes must be measured against their base in one session. Use the
following manual protocol until #129 supplies the synthetic-home,
cross-platform performance harness and a non-flaky regression budget.

1. Before building, name the full immutable commit IDs in the measurement
   record as `BASE_SHA` and `CHANGE_SHA`. Resolve both once, verify that each is
   a commit, and export each snapshot to a separate temporary source directory.
   Build separate binaries with the same Go toolchain and build flags:

   ```bash
   set -euo pipefail

   RUN_DIR=$(mktemp -d)
   BASE_SHA=$(git rev-parse --verify 'origin/main^{commit}')
   CHANGE_SHA=$(git rev-parse --verify 'HEAD^{commit}')

   test "$(git rev-parse --verify "$BASE_SHA^{commit}")" = "$BASE_SHA"
   test "$(git rev-parse --verify "$CHANGE_SHA^{commit}")" = "$CHANGE_SHA"
   mkdir "$RUN_DIR/base-src" "$RUN_DIR/change-src"
   git archive "$BASE_SHA" | tar -x -C "$RUN_DIR/base-src"
   git archive "$CHANGE_SHA" | tar -x -C "$RUN_DIR/change-src"
   (cd "$RUN_DIR/base-src" && go build -trimpath -o "$RUN_DIR/aibris-base" .)
   (cd "$RUN_DIR/change-src" && go build -trimpath -o "$RUN_DIR/aibris-change" .)
   ```

   Record `go version`, machine/OS, both SHAs, both binary checksums, and the
   build flags. Do not rebuild either binary during the run. Keep fail-fast and
   pipeline-failure handling enabled for all remaining measurement commands.

2. Freeze one measurement environment: the same machine, login session,
   measured `HOME`, normalized scan roots, command flags, environment variables,
   and background-work policy for both binaries. Record the exact root list and
   argv. For example, a one-root full-home run uses the same value for
   `<measured-home>` and `--root` every time:

   ```bash
   MEASURE_HOME=/absolute/path/to/measured-home
   BIN="$RUN_DIR/aibris-base"
   RUN_LOG="$RUN_DIR/warm-pair1-base.log"
   TIME_LOG="$RUN_DIR/warm-pair1-base.time"
   env HOME="$MEASURE_HOME" /usr/bin/time -p \
     "$BIN" scan --root "$MEASURE_HOME" > "$RUN_LOG" 2> "$TIME_LOG"
   ```

   Do not clean or otherwise mutate the measured home between runs. Preserve
   every run log and time log. The non-TTY scan log supplies the scanned source
   count and the summary supplies item and byte scale; record those values for
   every observation, not just once for the series. Admit an observation to a
   pair only when the timed scan exits zero; preserve failed logs separately,
   but never compute a delta from them.

3. Measure filesystem-cache conditions as separate series. For a **cold**
   series, apply the same documented, platform-appropriate cache-eviction
   procedure before every measured binary invocation. Record the exact command,
   its zero exit status, and a platform-specific signal that verifies eviction
   took effect before each invocation. If eviction fails, is a no-op, or cannot
   be verified, label that observation `uncontrolled` and exclude it from the
   cold median and range. A first run with unknown state is also
   `uncontrolled`, not `cold`.

   Track aibris application caches separately from the filesystem cache. A
   human-readable scan may read or refresh `codex-activity.json`; record its
   path, `created_at`, and checksum (or its absence) before and after every
   invocation. Establish one identical application-cache state for both
   binaries before a series. In every filesystem-cache condition, require the
   recorded application-cache identity to remain unchanged throughout each
   measured pair. If either binary refreshes or otherwise changes it, discard
   that pair and repeat after re-establishing a common state. For a **warm**
   series, first run one unmeasured warm-up of each binary, then perform the
   measured series without filesystem eviction. Never average cold, warm, or
   uncontrolled observations together.

4. Within each cache-condition series, alternate order by adjacent pairs. The
   minimum is four measured runs per condition:

   ```text
   pair 1: base,   change
   pair 2: change, base
   ```

   Additional repetitions continue `base/change`, `change/base`. This
   represents both orders and avoids completing all base runs before all change
   runs. Record the actual global run sequence; do not reconstruct it later.

5. For every temporally adjacent pair, compute
   `delta = change elapsed - base elapsed`, regardless of which binary ran
   first. Preserve every raw elapsed time and every delta. For each cache
   condition report the median paired delta and the range from minimum to
   maximum delta. A complete report has this shape:

   | Condition | Pair/order | Base SHA/time | Change SHA/time | Sources/items/bytes per run | Change−base |
   | --- | --- | --- | --- | --- | ---: |
   | cold | 1 / base→change | `<sha>` / … | `<sha>` / … | … | … |
   | cold | 2 / change→base | `<sha>` / … | `<sha>` / … | … | … |
   | warm | 1 / base→change | `<sha>` / … | `<sha>` / … | … | … |
   | warm | 2 / change→base | `<sha>` / … | `<sha>` / … | … | … |

   Follow the rows with the median and `[minimum, maximum]` paired delta for
   each condition. The summary describes this session only; it does not turn
   one timing or delta into a performance budget. Because a documentation-only
   change should have no scanner delta, investigate a material systematic
   difference instead of presenting it as an improvement.

Relay-driven sessions create and reclaim entries under `~/.relay/worktrees`
while work is in progress. Provider counts, item counts, byte totals, and
timings therefore drift even on one machine. Capture scale beside every timing
within the same run and session. If an adjacent pair's scale changes, flag it
and rerun after the working set stabilizes rather than silently treating it as
comparable. Stored counts and byte totals, like stored timings, are observations
and never targets.

No Make target is added by #146. A target that only alternates scans of a mutable
real home would conceal cache-state and working-set controls while duplicating
only a small, misleading part of the eventual harness. #129 remains the owner
of deterministic synthetic-home inputs, platform baselines, cache-control
automation, and a regression threshold that will not make CI flaky.

### Discovered vs. actual

Agent state stores aibris does **not** discover:

| Store | Actual | Nature |
| --- | ---: | --- |
| `~/.codex/sessions` | 11 GB | Conversation transcripts; 6,711 files, 85% older than 30d |
| `~/.codex/packages` | 1.0 GB | Single `standalone` entry; installed-vs-residue unconfirmed |
| `~/.relay/runs` | 933 MB | Executor run manifests |
| `~/.cursor/chats` | 674 MB | Conversation transcripts |
| `~/.codex/generated_images` | 548 MB | Agent-produced byproducts |
| `~/.config/superpowers/worktrees` | 516 MB | Two valid linked worktrees, never discovered |
| `~/.claude/projects` | 502 MB | Session store keyed by working directory |
| `~/.codex/sqlite` | 412 MB | Agent state database |
| `~/.codex/tmp` | 130 MB | Scratch |
| `~/.gstack/projects` | 91 MB | Per-project agent state |
| `~/.codex/computer-use` | 61 MB | Byproducts |
| `~/.cursor/ai-tracking` | 35 MB | Telemetry residue |
| Remainder (`~/.relay/reviews`, `~/.claude/session-env`, shell snapshots, …) | ~80 MB | |
| **Subtotal** | **≈ 16.0 GB** | **aibris discovers 0 B** |

Agent state stores aibris does discover:

| Store | Actual | aibris |
| --- | ---: | ---: |
| `~/.codex/logs_2.sqlite` | 1.3 GB | 1.41 GB |
| `~/.codex/worktrees` | 1.1 GB | 1.23 GB |
| `~/.cursor/projects` | 107 MB | 104 MB |
| `~/.codex/archived_sessions` | 85 MB | 89 MB |
| `~/.claude/command-audit.log` | 58 MB | 57 MB |
| `~/.claude/file-history` | 41 MB | 35 MB |
| **Subtotal** | **≈ 2.7 GB** | |

Coverage of the agent-produced surface is therefore **2.7 GB of ≈ 18.7 GB, or
about 15%**. Scoped to one tool, `aibris scan --root ~/.codex` reports 2.81 GB
against an actual 16 GB, missing 82%.

Meanwhile the same scan fully covers 16.6 GB of generic build debris —
`node_modules` 7.1 GB, Gradle cache 5.0 GB, uv cache 4.5 GB — which
general-purpose cleaners already handle.

### Default-path yield

`clean --dry-run` on the same home planned 4 targets totalling 1.4 GB, or 6.7%
of the 20.8 GB reported. Every target was `node_modules`. The agent-specific
categories contributed **0 B**:

| Category | Found | Eligible | Blocking reason |
| --- | ---: | ---: | --- |
| `node_modules` | 7.1 GB | 1.4 GB | younger than 7d |
| `build-cache` | 5.0 GB | 0 B | younger than 7d |
| `other-cache` | 4.5 GB | 0 B | younger than 7d |
| `worktree` | 2.7 GB | 0 B | active worktree protected |
| `ai-logs` | 1.6 GB | 0 B | requires `--risky` |

Guided review reported `selected 0 items 0 B` with all five rows locked or
reviewable. Lock reasons were activity within the 6-hour window (2), dirty or
untracked members (2), and recent-three repository retention (1). The policy is
internally correct; the outcome is empty.

Two structural causes are worth recording separately from the coverage gap:

- A global tool cache directory's mtime tracks continuous use, so it can never
  satisfy `--age 7d`. Age filtering permanently excludes global caches. By
  contrast a transcript file's mtime is the session end time and never changes,
  so age filtering is meaningful there. The same flag is precise in one place
  and structurally broken in the other.
- Guided review is codex-only. The largest single worktree on this home was a
  `claude` worktree, 1.5 GB, idle 93 days, reported `active` and therefore
  protected by the classic route with no guided path available.

### Orphan detection requires reading recorded cwd, not parsing names

Session stores key their directories by working directory, encoding the path
into a flat name. The encoding is lossy: `/`, `.`, and `_` all collapse to `-`.

```text
-Users-<user>--codex-worktrees-1bbd-<proj>  ->  ~/.codex/worktrees/1bbd/<proj>
Users-<user>-relay-worktrees-<hash>-<proj>  ->  ~/.relay/worktrees/<hash>/<proj>
```

Decoding a name back to a path is therefore not possible, and a first pass of
this audit misclassified live entries as orphans by attempting it. Every store
does, however, record an authoritative working directory internally:

| Store | Authoritative source |
| --- | --- |
| `~/.claude/projects/<key>/*.jsonl` | `cwd` field |
| `~/.cursor/projects/<key>/worker.log` | first non-`.cursor` absolute path |
| `~/.codex/sessions/<y>/<m>/<d>/rollout-*.jsonl` | `cwd` field |

Re-measured with the recorded `cwd`:

| Store | Live | Orphaned (cwd absent) | Undetermined |
| --- | ---: | ---: | ---: |
| `~/.claude/projects` | 42 / 358 MB | **81 / 162 MB** | 11 / 0.1 MB |
| `~/.cursor/projects` | 79 / 77 MB | **42 / 31.5 MB** | 11 / 0.8 MB |

The orphans are overwhelmingly former agent worktree paths under
`~/.relay/worktrees`, `~/.codex/worktrees`, project-local `.claude/worktrees`,
and a removed `~/orca/workspaces` tree. The pattern is consistent: an agent
created a worktree, the worktree was reclaimed, and the session store kept a
directory keyed to a path that no longer exists.

An absent `cwd` is a proof rather than a heuristic, and resume is already
impossible for those entries, so this class needs no age gate to be safe.

### Session store age distribution

`~/.codex/sessions` is not orphaned, only old. It needs a retention decision
rather than a safety decision:

| Period | Size |
| --- | ---: |
| 2025 (Sep–Dec) | 66 MB |
| 2026-01 | 76 MB |
| 2026-02 | 1.7 GB |
| 2026-03 | 2.1 GB |
| 2026-04 | 1.2 GB |
| 2026-05 | 1.5 GB |
| 2026-06 | 1.6 GB |
| 2026-07 | 2.6 GB |

Transcripts are user content. The product obligation is to surface these
buckets, not to reclaim them by default.

### Conclusion

aibris covers most of a generic cleaner's territory and about 15% of its own.
The gap is provider coverage, not policy or rendering. Epic #137 and issues
#138–#143 track closing it.
