---
id: AIB-142
title: Add agent byproduct store providers
status: Blocked
labels:
  - enhancement
  - cli
  - scanner
  - type:feature
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
---
## Description

## Goal

Classify the uncovered stores first, add only safety-bounded regenerable
coverage next, and defer protected-store inventory to the #139 retention
contract. The original `~/.codex/packages` 1.0 GB, `generated_images` 548 MB,
`sqlite` 412 MB, `tmp` 130 MB, `computer-use` 61 MB, and
`~/.cursor/ai-tracking` 35 MB figures are preserved 2026-07-26 observations,
not size, coverage, cleanup, or retention targets.

Installed/regenerable/protected are issue-planning taxonomy only. They are not
current categories, agent-state classifications, JSON fields, or CLI selectors.

## Accepted L1/L2/L3 split

- [x] **L1 — store classification (documentation only):** freeze the evidence,
      store nature, and downstream policy before adding a provider.
- [ ] **L2 — regenerable provider:** consider only direct child units of
      `~/.codex/tmp`. The observed `path/` child is evidence, not a stable name
      or allowlist, and its `codex-arg*` descendants are not independent units.
      Enumerate every direct child, admit only versioned layouts that pass the
      ownership and active-use/TOCTOU contract below, surface unsupported
      children as protected and ineligible, and never delete the tmp root.
- [ ] **L3 — protected inventory:** does not wait on #139. The re-scoped #139
      ships only a read-only inventory with no retention-selection contract, so
      protected stores remain inventory-only and non-executable until a future
      leaf ships the explicit selection machinery.

## Frozen store decisions

| Store | Decision | Provider and cleanup consequence |
| --- | --- | --- |
| `~/.codex/packages` | Installed content | No provider; excluded from inventory and every cleanup surface. |
| `~/.codex/computer-use` | Installed content | No provider; excluded from inventory and every cleanup surface. |
| `~/.codex/tmp` | Regenerable residue | Currently undiscovered, unselectable, and ineligible. Only direct children are future safety-bounded default-clean units. Their basenames do not establish identity; each whole child must pass the versioned contract below, and the root must never be deleted. |
| `~/.codex/generated_images` | Protected content | Not default-clean and not deletable through `--risky` alone. Explicit retention-selection machinery remains parked with #139's re-scope; the read-only inventory contract does not make it selectable. |
| `~/.codex/sqlite` | Protected content | Inventory-only unless a separate future implementation satisfies the fail-closed quiescence, family-registry, and atomic-manifest contract below. |
| `~/.cursor/ai-tracking` | Protected content | Inventory-only unless a separate future implementation satisfies the fail-closed quiescence, family-registry, and atomic-manifest contract below. |

L2 and L3 are blocked, not locally implementable leaves. In addition to the
(no-longer-applicable) #139 L1 gate, each relevant upstream producer must first
expose or document a producer-documented, versioned layout/identity contract
plus a cooperative
lock, lease, shutdown, or pause/fencing protocol honored by every writer. That
is the unblock signal for the existing fail-closed proofs; aibris cannot
manufacture producer cooperation locally. If the signal never exists, affected
tmp units remain ineligible and affected protected stores remain
non-executable and inventory-only indefinitely.

### L2 safety prerequisite

Each supported Codex release/channel and tmp layout needs a versioned
recognizer and fixtures. Ownership requires a canonical, non-symlink direct
child whose complete contents are accounted for by producer-issued identity
evidence or a documented upstream layout tied to the detected version. The
basename, age, shim names, process name, and absence of a current writer are
insufficient.

Before enumeration, L2 must acquire a producer-cooperative exclusive lock,
lease, or pause handshake with an observable ownership or fencing token. It
must be honored by the Codex GUI application, Codex CLI, apply-patch launchers
and callers, and Codex/agent supervisor or helper processes. Every descendant
symlink and its target is inventoried; an unknown target keeps the whole child
protected. While exclusion is held, L2 snapshots the canonical path, file
identities, complete member set, entry types, link targets, sizes, and
modification times. Immediately before mutation it re-enumerates and compares
the unit and token, including canonical-path and link-target revalidation.
An eligible direct-child unit may be removed recursively, including its
contained ordinary files and directories. For each symlink encountered inside
the unit, deletion unlinks only the symlink directory entry and never follows,
traverses, removes, or mutates the symlink target. An unknown layout or writer,
a writer that does not participate, lock loss, or any mismatch leaves the
entire child protected with no partial deletion or byte credit. Process-name,
`lsof`, `/proc`, and open-handle checks alone cannot prove exclusion. Fixtures
and platform race tests must cover unknown children and creation, mutation,
rename, and lock loss from every writer class.

### L3 safety prerequisite

Database-family enumeration uses an explicitly open, versioned registry rather
than a closed suffix list. The registry scans the bounded store directory and
assigns every entry to one primary family or proves it unrelated. It starts
with the engine-companion conventions `-wal`, `-shm`, `-journal`, `.wal`,
`.shm`, and `.journal`. `.backup` and `.bak` are independent protected
artifacts, not SQLite engine companions; only a later store-specific, versioned
contract may separately prove and register an association. The registry remains
open: every newly observed store-specific journal, backup, or sidecar convention
requires a registry and fixture update before inventory resumes. An unassigned
entry makes the store incomplete, protected, and non-inventoriable.

A complete family is the primary plus every registry-assigned member. Process
quiescence is a producer-cooperative exclusive lock, lease, shutdown, or pause
protocol with an observable ownership or fencing token. Each supported
store/version must register every application, database connection, indexing or
sync worker, and agent or supervisor helper capable of writing the store; all
must honor the protocol. The token is acquired before the first enumeration,
revalidated before and after each publication step, and held until publication
and directory durability are confirmed. An unknown or non-participating writer
makes quiescence unprovable. Point-in-time process or open-handle enumeration is
never sufficient.

Under that exclusion, one immutable manifest records every member's canonical
path, file identity, size, and modification time. L3 syncs a temporary
manifest, re-enumerates and compares the family and token, atomically replaces
the same-directory destination, makes the directory entry durable, and verifies
the token again. It emits or accepts the manifest as inventory only after every
step succeeds; files from interrupted or previous attempts are never inventory
evidence. Lock loss or another mid-publish violation, a non-participating
writer, membership or metadata drift, publication failure, or a platform
without the required atomic-replace and directory-durability primitives aborts
the operation, removes attempt files where exclusion still permits it, and
emits no inventory. Platform and fault-injection tests must cover those paths,
including lock loss before and after replacement. The future L3 contract must
define a supported-platform/filesystem capability matrix and runtime probes for
atomic replacement and directory durability; a missing or failed capability is
an abort condition.

Uncertainty resolves to protected content, never broader cleanup eligibility.
This split does not add a provider or define the protected-content category,
selector, retention bucket, or execution manifest reserved for #139.

## Remaining acceptance criteria

- [ ] L2 implements the versioned ownership, cooperative exclusion,
      revalidation, and race-test contract for each admitted direct-child
      layout before registering a tmp provider.
- [ ] L3 preserves each store-specific consequence above; it does not depend
      on #139 (the re-scoped #139 ships only a read-only inventory, with the
      retention selector/manifest/executor parked).
- [ ] Provider changes preserve the existing cache, JSON, CLI, eligibility, and
      deletion-safety contracts.

Issue #142 remains open. Reconciliation of the GitHub issue body is the
orchestrator's responsibility after the classification PR merges.
