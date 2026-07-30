# Evidence-Based Reclamation Dogfood

The initial notes record the sanitized evidence used for issue #90; later
sections add separate read-only coverage and classification audits. The real
`$HOME` exercises were limited to scan, metadata/Git inspection, and dry-run
planning. The only deletion was a disposable branch-backed linked worktree
under a temporary `HOME`; all temporary repositories and worktrees were
removed afterward.

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
   minimum is eight measured runs per condition: four adjacent pairs, with at
   least two pairs in each order:

   ```text
   pair 1: base,   change
   pair 2: change, base
   pair 3: base,   change
   pair 4: change, base
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
   | cold | 3 / base→change | `<sha>` / … | `<sha>` / … | … | … |
   | cold | 4 / change→base | `<sha>` / … | `<sha>` / … | … | … |
   | warm | 1 / base→change | `<sha>` / … | `<sha>` / … | … | … |
   | warm | 2 / change→base | `<sha>` / … | `<sha>` / … | … | … |
   | warm | 3 / base→change | `<sha>` / … | `<sha>` / … | … | … |
   | warm | 4 / change→base | `<sha>` / … | `<sha>` / … | … | … |

   Follow the rows with the median and `[minimum, maximum]` paired delta for
   each condition. The summary describes this session only; it does not turn
   one timing or delta into a performance budget. Unless a stability or
   uncertainty rule was declared before the run, even the minimum series is
   `inconclusive` as a regression decision; preserve it as an observation and
   collect more pairs. #129 owns the repeatable harness and non-flaky decision
   threshold rather than letting an operator invent one after seeing results.
   Because a documentation-only change should have no scanner delta,
   investigate a material systematic difference instead of presenting it as an
   improvement.

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

### 2026-07-30 Registered Superpowers Coverage Observation

Issue #140 remeasured `~/.config/superpowers/worktrees` in one read-only
session. This is evidence for the finite exact registry, not a fixture target.
Only `scan` was run against the real home; no real-home `clean` command was run.

| Evidence | Observation |
| --- | --- |
| Filesystem outer owners | 1 directory |
| Direct / one-level linked members | 0 direct / 2 one-level; both metadata references active |
| Unique physical owner bytes | 540,565,504 B (`du -sk`: 527,896 KiB) |
| Base full-HOME scan | 320 items / 33,556,803,863 B overall; 0 superpowers rows |
| Pre-safety-fix full-HOME scan | 362 items / 34,641,563,927 B overall; 2 superpowers rows |
| Pre-safety-fix scoped scan | 4 items / 1,412,632,576 B overall; the same 2 superpowers rows |
| Superpowers attribution | `source=superpowers`, `tool=unknown`, 2 active logical member rows |
| Raw superpowers row-size sum | 1,081,131,008 B because both logical rows carry the shared owner size |

The historical pre-safety-fix full and scoped superpowers keys matched exactly by source,
tool, owner path, project, status, and size. Physical accounting remains one owner /
540,565,504 B; summing the two compatibility rows double-counts that owner and
must be labelled raw row-size aggregation.

#### Immutable inputs and fixed run contract

Both source trees were exported with `git archive` and built once with the
identical command `go build -trimpath -o <binary> .`; neither binary was rebuilt
during the run:

| Input | Exact value |
| --- | --- |
| `BASE_SHA` | `41cab283fbc1147d59b3af53bec48fa6163f9f20` |
| Base binary SHA-256 | `5546a85e12e326f2b4993243b3233f2632df22954c62eafa8c0b0a416695058b` |
| `PRE_SAFETY_FIX_SHA` | `ee056aeff371fc80ba4e5d1922b9e5539ff1bab3` |
| Pre-safety-fix binary SHA-256 | `2e90cadd6f90c7d15f8d19d28bf4d74308e0d1ac2214f4af336fa2083e84f77b` |
| Toolchain | `go version go1.26.3 darwin/arm64`; GOROOT Go 1.26.3 |
| Machine / OS | Apple arm64; macOS 26.5.2 (25F84) |
| Exact argv | `<immutable-binary> scan --root /Users/sjlee --json` |
| Exact environment | `env -i HOME=/Users/sjlee PATH=/usr/bin:/bin:/usr/sbin:/sbin TMPDIR=/tmp LANG=C LC_ALL=C` |
| Application cache | `/Users/sjlee/Library/Caches/aibris/codex-activity.json`; SHA-256 `bf48b6d973392f6e42f3dc0e5bff9b42f031b3f957b04ab748d290a38485b63c`; 3,029,853 B; `created_at=2026-07-30T21:24:45.354487+09:00` |

This series was the final binary series for the initial registry pass, but
internal safety review subsequently found the mixed active/orphaned physical
owner defect. It is retained as historical evidence and is superseded by the
immutable repair series below. The application-cache identity above was
captured before and after every measured invocation and remained
byte-identical. The login session and background-work policy were unchanged,
and the measured home was not deliberately mutated between invocations.

This is a warm series. There was no cache eviction and no cold claim. One
unmeasured exact-argv warm-up of each immutable binary completed first: base
13.98 s at 8 sources / 318 items / 32,650,865,738 B, then change 18.90 s at
8 / 359 / 33,735,625,802 B. A following base-only 57.76 s reporting-harness
attempt (8 / 318 / 32,650,864,942 B) was excluded before pairing because a
reserved shell variable aborted the recorder after the scan; the adjacent
sequence was restarted from pair 1.

#### Alternating adjacent warm pairs

Times are `/usr/bin/time -p` wall-clock `real` seconds. Each scale cell is
`sources/items/bytes`. `change-base` is computed regardless of invocation
order. Every invocation exited zero with `partial=false` and zero provider
errors. Expected final-only worktree inventory rows are not drift; a shared
row size change or a non-worktree row present in only one adjacent invocation
is drift and discards that pair.

| Pair / order | Base time; scale | Change time; scale | change-base | Decision |
| --- | --- | --- | ---: | --- |
| 1 / base→change | 126.62 s; 8/318/32,650,864,942 | 143.59 s; 8/360/33,735,653,678 | +16.97 s | discarded: `~/workspace/active/home-stack/ridi-to-md/node_modules` appeared; `~/.relay/worktrees/0e6abc57` changed +28,672 B |
| 2 / change→base | 91.46 s; 8/319/32,650,893,614 | 115.85 s; 8/360/33,735,653,678 | +24.39 s | accepted |
| 3 / base→change | 78.57 s; 8/319/32,660,265,190 | 128.19 s; 8/360/33,747,360,199 | +49.62 s | discarded: `~/.npm/_cacache` changed +2,334,945 B |
| 4 / change→base | 62.81 s; 8/320/33,075,556,631 | 64.36 s; 8/360/33,753,514,263 | +1.55 s | discarded: `~/workspace/active/writer-stack/blog/node_modules` appeared at 406,802,432 B |
| 5 / base→change | 79.86 s; 8/320/33,262,752,023 | 109.82 s; 8/361/34,347,516,183 | +29.96 s | discarded: `~/.relay/worktrees/0e6abc57` changed +4,096 B |
| 6 / change→base | 92.65 s; 8/319/33,255,391,511 | 71.00 s; 8/361/34,340,151,575 | -21.65 s | accepted |
| 7 / base→change | 80.13 s; 8/320/33,262,760,215 | 66.28 s; 8/362/34,347,602,199 | -13.85 s | discarded: `~/.relay/worktrees/b9bf64e7` changed +81,920 B |
| 8 / change→base | 56.53 s; 8/320/33,262,866,711 | 81.15 s; 8/362/34,347,622,679 | +24.62 s | discarded: `~/.relay/worktrees/b9bf64e7` changed -4,096 B |
| 9 / base→change | 60.01 s; 8/320/33,262,874,903 | 61.30 s; 8/362/34,347,639,063 | +1.29 s | discarded: `~/.relay/worktrees/37b00ead` changed +4,096 B |
| 10 / change→base | 82.71 s; 8/320/33,512,370,455 | 68.51 s; 8/362/34,347,639,063 | -14.20 s | discarded: `~/workspace/active/writer-stack/blog/node_modules` changed -249,491,456 B |
| 11 / base→change | 76.94 s; 8/320/33,529,483,543 | 68.80 s; 8/362/34,641,563,927 | -8.14 s | discarded: two `node_modules` rows changed +12,288 B and +27,308,032 B |
| 12 / change→base | 63.24 s; 8/320/33,556,803,863 | 62.04 s; 8/362/34,641,563,927 | -1.20 s | accepted |
| 13 / base→change | 73.75 s; 8/320/33,556,803,863 | 80.77 s; 8/362/34,641,563,927 | +7.02 s | accepted |
| 14 / change→base | 67.47 s; 8/320/33,556,803,863 | 96.32 s; 8/362/34,641,563,927 | +28.85 s | accepted |
| 15 / base→change | 67.10 s; 8/320/33,556,803,863 | 61.81 s; 8/362/34,641,563,927 | -5.29 s | accepted |

The six drift-free adjacent pairs contain two `base→change` and four
`change→base` orders. Their retained deltas are -21.65, -5.29, -1.20, +7.02,
+24.39, and +28.85 seconds: median **+2.91 s**, range
**[-21.65 s, +28.85 s]**. The final four-pair stabilized block (pairs 12–15)
alone has two of each order and identical per-binary scale on every invocation.
No regression threshold or stability rule was predeclared, so this observation
is **inconclusive**, not a performance pass or improvement claim.

#### 2026-07-31 immutable final mixed-owner safety correction

The binary-affecting correction was committed before measurement. The final
repair source was exported with `git archive` and built once with the same
flags as the preserved base: `go build -trimpath -o <binary> .`. The preserved
base binary was reused only after its documented SHA-256 verified successfully;
neither immutable binary was rebuilt during this series.

| Input | Exact value |
| --- | --- |
| `BASE_SHA` | `41cab283fbc1147d59b3af53bec48fa6163f9f20` |
| Base binary SHA-256 | `5546a85e12e326f2b4993243b3233f2632df22954c62eafa8c0b0a416695058b` |
| `FINAL_REPAIR_SHA` | `e65ab5220ec17b30e9c21f181ef9146f098f5ffc` |
| Final repair binary SHA-256 | `47f0e7f0de1818576851c621769a880b617d3762443e54a463dc6b999aea60e6` |
| Binary sizes | base 5,579,826 B; final repair 5,613,970 B |
| Toolchain | `go version go1.26.3 darwin/arm64`; GOROOT Go 1.26.3 |
| Machine / OS | Apple arm64; macOS 26.5.2 (25F84) |
| Exact argv | `<immutable-binary> scan --root /Users/sjlee --json` |
| Exact environment | `env -i HOME=/Users/sjlee PATH=/usr/bin:/bin:/usr/sbin:/sbin TMPDIR=/tmp LANG=C LC_ALL=C` |
| Cache identity `K1` | `/Users/sjlee/Library/Caches/aibris/codex-activity.json`; SHA-256 `bf48b6d973392f6e42f3dc0e5bff9b42f031b3f957b04ab748d290a38485b63c`; 3,029,853 B; `created_at=2026-07-30T21:24:45.354487+09:00` |

The cache SHA-256 in `K1` is
`bf48b6d973392f6e42f3dc0e5bff9b42f031b3f957b04ab748d290a38485b63c`.
It was captured before and after every warm-up and every measured invocation;
all 20 captures were exactly `K1`. Every scan exited zero with
`partial=false` and zero provider errors. Only real-home `scan` was run; no
real-home `clean` command was used.

This is a warm-only series: there was no cache eviction and no cold claim.
Both immutable binaries were warmed with the exact measured argv before the
paired sequence:

| Invocation | Time; scale (`sources/items/bytes`) | Cache before→after |
| --- | --- | --- |
| warm-up base | 54.26 s; 8/320/33,651,012,698 | `K1`→`K1` |
| warm-up final repair | 50.38 s; 8/362/34,735,772,762 | `K1`→`K1` |

The exact global measured order was base, repair, repair, base, base, repair,
repair, base. Times are `/usr/bin/time -p` wall-clock `real` seconds and
`repair-base` is computed regardless of invocation order.

| Pair / order | Base time; scale | Final repair time; scale | Cache identities | repair-base | Drift decision |
| --- | --- | --- | --- | ---: | --- |
| 1 / base→repair | 54.49 s; 8/320/33,651,012,698 | 56.96 s; 8/362/34,735,772,762 | base `K1`→`K1`; repair `K1`→`K1` | +2.47 s | accepted |
| 2 / repair→base | 63.93 s; 8/320/33,651,012,698 | 61.06 s; 8/362/34,735,772,762 | base `K1`→`K1`; repair `K1`→`K1` | -2.87 s | accepted |
| 3 / base→repair | 50.93 s; 8/320/33,651,012,698 | 48.04 s; 8/362/34,735,772,762 | base `K1`→`K1`; repair `K1`→`K1` | -2.89 s | accepted |
| 4 / repair→base | 49.41 s; 8/320/33,651,012,698 | 74.47 s; 8/362/34,735,772,762 | base `K1`→`K1`; repair `K1`→`K1` | +25.06 s | accepted |

The drift rule was unchanged from the historical series: the 42 repair-only
worktree rows are the expected registry effect; any base-only row, non-worktree
repair-only row, or shared-row size change rejects a pair. Every pair had
exactly 0 base-only rows, 42 repair-only worktree rows, 0 one-sided
non-worktree rows, and 0 shared-row size changes. The per-binary canonical
inventory signatures were also identical across all four appearances: base
`34bd8daff4a1d00a4d60a664d2f3a680fa23f5202ade02900f90b3fbab655d87`,
repair
`c541691420e7d16c6e522a4d03f37960cfb61c00d3ec5224b07b66b1ade1a2c0`.

The four retained deltas are -2.89, -2.87, +2.47, and +25.06 seconds:
median **-0.20 s**, range **[-2.89 s, +25.06 s]**. There was no predeclared
regression threshold or stability rule, and the absolute timing spread remains
large. The final correction series is therefore **inconclusive**, not a
performance pass, regression, or improvement claim.

##### Final repair full/scoped correctness observation

A following read-only correctness session reused
`/private/tmp/aibris-140-safety-final.r3PYkh/aibris-change` unchanged. Its
SHA-256 still matched
`47f0e7f0de1818576851c621769a880b617d3762443e54a463dc6b999aea60e6`.
The preserved `change-src` tree was byte-for-byte equal to
`git archive e65ab5220ec17b30e9c21f181ef9146f098f5ffc`, and an independent
`go build -trimpath` reproducibility check produced the same binary SHA-256.
The accepted four-pair performance series above was not rerun or replaced.

Both correctness invocations used the same fixed environment documented above.
The full scan ran from `2026-07-30T15:38:39Z` through
`2026-07-30T15:39:22Z` (`2026-07-31T00:38:39+09:00` through
`2026-07-31T00:39:22+09:00`). The scoped scan immediately followed from
`2026-07-30T15:39:22Z` through `2026-07-30T15:39:23Z`
(`2026-07-31T00:39:22+09:00` through `2026-07-31T00:39:23+09:00`).

| Invocation | Exact argv suffix | Overall scale (`sources/items/bytes`) | Completeness |
| --- | --- | --- | --- |
| Full HOME | `scan --root /Users/sjlee --json` | 8 / 362 / 34,735,772,762 B | exit 0; `partial=false`; 0 provider errors |
| Scoped superpowers | `scan --root /Users/sjlee/.config/superpowers/worktrees --json` | 8 / 4 / 1,412,632,576 B | exit 0; `partial=false`; 0 provider errors |

Cache identity was `K1` before the full scan and after the scoped scan: SHA-256
`bf48b6d973392f6e42f3dc0e5bff9b42f031b3f957b04ab748d290a38485b63c`,
3,029,853 B, `created_at=2026-07-30T21:24:45.354487+09:00`. Its hash, size,
and filesystem mtime remained unchanged across the session.

The final repair full-HOME and scoped superpowers rows match exactly by
`source`, `tool`, physical owner `path`, `project`, `status`, and `size`.
Their sorted key documents have the same SHA-256,
`c23d07c115c6a5c1255f07d7fb316de5f8c68bb20081b02d4028a2fa6f723075`:

| Project | Source / tool | Physical owner | Status | Per-row size |
| --- | --- | --- | --- | ---: |
| `ds121-question-repair` | `superpowers` / `unknown` | `~/.config/superpowers/worktrees/dear-scene` | `active` | 540,565,504 B |
| `m61-feedback-loop` | `superpowers` / `unknown` | `~/.config/superpowers/worktrees/dear-scene` | `active` | 540,565,504 B |

The accounting dimensions are deliberately separate:

| Dimension | Full HOME | Scoped |
| --- | ---: | ---: |
| Logical superpowers rows | 2 | 2 |
| Unique physical owners | 1 | 1 |
| Unique physical owner bytes | 540,565,504 B | 540,565,504 B |
| Raw row-size sum | 1,081,131,008 B | 1,081,131,008 B |

The read-only filesystem oracle ran from `2026-07-30T15:39:48Z` through
`2026-07-30T15:39:49Z` (`2026-07-31T00:39:48+09:00` through
`2026-07-31T00:39:49+09:00`). The registered container had one outer owner,
`dear-scene`, at 527,896 KiB / 540,565,504 B. It contained the two one-level
`.git` marker files named by the logical rows, and both referenced gitdirs
existed. This independently confirms the one-owner/two-member active
inventory without mutating the container.

The terminal correction commit containing this test and documentation update
is test/docs-only. `e65ab5220ec17b30e9c21f181ef9146f098f5ffc` remains the
final binary-affecting source and the immutable final repair binary identity
remains unchanged.

The prior audit's preserved `516 MB` label came from the 2026-07-26 observation;
it was never an implementation or performance target.

### Discovered vs. actual

Agent state stores aibris does **not** discover:

| Store | Preserved 2026-07-26 size observation | Contents |
| --- | ---: | --- |
| `~/.codex/sessions` | 11 GB | Conversation transcripts; 6,711 files, 85% older than 30d |
| `~/.codex/packages` | 1.0 GB | Standalone versioned release installation |
| `~/.relay/runs` | 933 MB | Executor run manifests |
| `~/.cursor/chats` | 674 MB | Conversation transcripts |
| `~/.codex/generated_images` | 548 MB | Generated PNG artifacts |
| `~/.claude/projects` | 502 MB | Session store keyed by working directory |
| `~/.codex/sqlite` | 412 MB | Codex application SQLite databases and sidecars |
| `~/.codex/tmp` | 130 MB | Temporary apply-patch shim store |
| `~/.gstack/projects` | 91 MB | Per-project agent state |
| `~/.codex/computer-use` | 61 MB | Codex Computer Use application bundle |
| `~/.cursor/ai-tracking` | 35 MB | Tracked-file and conversation-summary database |
| Remainder (`~/.relay/reviews`, `~/.claude/session-env`, shell snapshots, …) | ~80 MB | |
| **Preserved 2026-07-26 subtotal after moving superpowers below** | **≈ 15.5 GB** | **aibris discovers 0 B** |

#### 2026-07-31 issue #142 bounded store evidence

This separate read-only observation used shallow names, filesystem metadata,
application identity, and SQLite schema names. Its sizes and counts describe one
changing developer home at the time of observation; they are evidence about
store shape, never cleanup, coverage, performance, or retention targets.

| Store | Point-in-time metadata/schema-name evidence | Conservative decision and downstream policy |
| --- | --- | --- |
| `~/.codex/packages` | 1,027,964 KiB observed. `standalone` contained `install.lock`, four versioned architecture release directories, and a `current` symlink to the active version. | Installed content. Exclude it from providers, inventory, and cleanup. |
| `~/.codex/computer-use` | 62,168 KiB observed. `Codex Computer Use.app` reported bundle identity `com.openai.sky.CUAService`. | Installed content. Exclude it from providers, inventory, and cleanup. |
| `~/.codex/tmp` | 132,692 KiB observed. The literal `~/.codex/tmp/path/` directory had 4,345 direct `codex-arg*` directories, each with paired `applypatch` and `apply_patch` symlink entries. This observation did not establish `path/` as an upstream-stable name. | Regenerable residue, but only a future safety-bounded default-clean candidate. `path/` was one observed direct-child unit; its descendants were not independent units, and its name is not an allowlist. A future L2 must enumerate every direct child, fail closed on unsupported layouts, satisfy the ownership and active-use/TOCTOU contract in `CATEGORY.md`, and never delete the tmp root. |
| `~/.codex/generated_images` | 561,636 KiB observed across 16 ID directories and 414 `.png` files. | Protected user artifacts. Only explicit retention selection after merged #139 L1 may be considered; default clean and `--risky` alone are insufficient. |
| `~/.codex/sqlite` | 422,320 KiB observed. Filenames identified goals, memories, logs, history snapshots, and state databases; readable schema names included `thread_goals`, `threads`, `agent_jobs`, `app_server_history_snapshots`, and `logs`. WAL/SHM siblings were present for live database families. | Protected live state. A future provider is inventory-only unless the separate contract proves process quiescence and supplies the complete, atomically published database-family manifest defined in `CATEGORY.md`. |
| `~/.cursor/ai-tracking` | 36,004 KiB observed. `ai-code-tracking.db` schema names included `tracked_file_content`, `conversation_summaries`, `scored_commits`, `ai_deleted_files`, and `tracking_state`. | Protected content and provenance, not disposable telemetry. It has the same inventory-only, quiescence, complete-family, and atomic-manifest boundary as Codex SQLite. |

No conversation body, SQLite row value, generated image pixel, tracked file
content, or other content body was inspected. Metadata and schema-name evidence
was sufficient for these conservative decisions; uncertainty would have
resolved to protected content. No `aibris clean` command was needed or run for
this classification.

Agent state stores aibris does discover:

| Store | Actual | aibris |
| --- | ---: | ---: |
| `~/.codex/logs_2.sqlite` | 1.3 GB | 1.41 GB |
| `~/.codex/worktrees` | 1.1 GB | 1.23 GB |
| `~/.cursor/projects` | 107 MB | 104 MB |
| `~/.codex/archived_sessions` | 85 MB | 89 MB |
| `~/.claude/command-audit.log` | 58 MB | 57 MB |
| `~/.claude/file-history` | 41 MB | 35 MB |
| `~/.config/superpowers/worktrees` | 540,565,504 B current physical observation | 2 logical rows / 1,081,131,008 B raw row sum; 540,565,504 B unique owner |
| **Preserved 2026-07-26 subtotal, excluding the current row** | **≈ 2.7 GB** | |

The stored 2.7 GB, 18.7 GB, 15%, 2.81 GB, and 16 GB figures are preserved
2026-07-26 observations, not current coverage targets. The current superpowers
row above is deliberately reported with both logical/raw and unique physical
accounting instead of silently adding its duplicated row bytes to those
historical totals.

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
The 2026-07-26 audit originally described the remainder as a provider-coverage
gap. Issue #142's later classification narrows that statement: installed
content is intentionally excluded, tmp is only a future safety-bounded
candidate, and protected stores first need inventory/retention contracts rather
than generic cleanup providers. Epic #137 and issues #138–#143 track those
separate obligations.
