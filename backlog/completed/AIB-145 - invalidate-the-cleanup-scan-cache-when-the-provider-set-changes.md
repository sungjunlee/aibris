---
id: AIB-145
title: Invalidate the cleanup scan cache when the provider set changes
status: Done
labels:
  - bug
  - cli
  - scanner
  - safety
  - type:bug
priority: high
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-28'
---
## Description
## Goal

Make the `scan` cleanup cache invalid when the producing binary's provider set differs from the consuming binary's, so `clean` never plans from an inventory a different version produced.

## Evidence

Found while dogfooding #138. A build with the new `agent-state` provider reused a cache written moments earlier by a build without it:

```console
$ aibris-new clean --no-guide --category agent-state --dry-run
  scan    cached, 18s old
  scanned    8 sources   159 items   21.0 GB
  matched  0 candidates   0 B
No items to clean.
```

A fresh scan with the same new build finds 293 items including 134 `agent-state` items. The cache silently suppressed an entire category, and the by-category table did not list `agent-state` at all — so the output looked like a definitive "nothing to clean" rather than a stale read.

## Why this matters now

`docs/SPEC.md` says the snapshot is reused when "the scan roots and cache schema match". The **provider set** is not part of that key, and it determines what the inventory can contain. Today that is a narrow window — a user would have to upgrade between `scan` and `clean` inside the 5-minute freshness bound.

Epic #137 widens it considerably: it adds four to five providers across #138, #139, #140, and #142. Every release in that sequence changes the provider set, so the upgrade path is exactly where users will hit it, and the failure is silent under-reporting on a command whose whole job is to be trustworthy about what it found.

## Acceptance criteria

- [ ] The cleanup scan cache records enough about the producing binary's provider set that a consumer with a different set treats the snapshot as incompatible.
- [ ] An incompatible snapshot falls back to a live scan with the existing visible `scan live` progress path, rather than failing.
- [ ] The chosen key survives provider reordering and does not churn on unrelated changes — prefer a derived, sorted identity over a hand-maintained version constant that a future provider author must remember to bump.
- [ ] A test writes a snapshot with one provider set, reads it with another, and asserts a live-scan fallback.
- [ ] A test asserts an unchanged provider set still reuses a fresh snapshot, so the existing fast path is preserved.
- [ ] `docs/SPEC.md` states the provider set as part of the cache compatibility contract.

## Out of scope

- Changing the 5-minute freshness bound or the roots-matching rule.
- The versioned scan JSON schema, which is #124.
