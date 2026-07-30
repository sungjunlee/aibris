---
id: AIB-142
title: Add agent byproduct store providers
status: To Do
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
      `~/.codex/tmp` after proving ownership and active-use/TOCTOU safety. Never
      delete the whole tmp root.
- [ ] **L3 — protected inventory:** start only after #139 L1 merges, then follow
      each protected store's policy below without making `--risky` a deletion
      unlock.

## Frozen store decisions

| Store | Decision | Provider and cleanup consequence |
| --- | --- | --- |
| `~/.codex/packages` | Installed content | No provider; excluded from inventory and every cleanup surface. |
| `~/.codex/computer-use` | Installed content | No provider; excluded from inventory and every cleanup surface. |
| `~/.codex/tmp` | Regenerable residue | Currently undiscovered, unselectable, and ineligible. It is only a future safety-bounded default-clean candidate; L2 is limited to safety-proven direct child units and must not delete the root. |
| `~/.codex/generated_images` | Protected content | Not default-clean and not deletable through `--risky` alone. Explicit retention selection may be considered only after #139 L1 merges. |
| `~/.codex/sqlite` | Protected content | Inventory-only unless a separate future contract proves process quiescence and one atomic manifest for every database/WAL/SHM family. |
| `~/.cursor/ai-tracking` | Protected content | Inventory-only unless a separate future contract proves process quiescence and one atomic manifest for every database/WAL/SHM family. |

Uncertainty resolves to protected content, never broader cleanup eligibility.
This split does not add a provider or define the protected-content category,
selector, retention bucket, or execution manifest reserved for #139.

## Remaining acceptance criteria

- [ ] L2 proves its unit boundary, ownership, and active-use/TOCTOU behavior
      before registering a tmp provider.
- [ ] L3 waits for merged #139 L1 semantics and preserves each store-specific
      consequence above.
- [ ] Provider changes preserve the existing cache, JSON, CLI, eligibility, and
      deletion-safety contracts.

Issue #142 remains open. Reconciliation of the GitHub issue body is the
orchestrator's responsibility after the classification PR merges.
