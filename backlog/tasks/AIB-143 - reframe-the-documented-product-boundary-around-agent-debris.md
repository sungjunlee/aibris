---
id: AIB-143
title: Reframe the documented product boundary around agent debris
status: In Review
labels:
  - documentation
  - docs
  - area:docs
  - area:oss
  - ux
  - type:chore
priority: medium
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-26'
---
## Description
## Goal

State the boundary honestly: aibris cleans agent state stores and complements
general-purpose cleaners rather than competing with them. The audit found ~16.6 GB
of generic build debris fully covered against ~15% coverage of aibris's own
subject, and the docs present both as equally central.

## Acceptance criteria

- [x] README leads with agent state store cleanup; generic build debris is
      described as complementary coverage that keeps `scan` a complete picture.
- [x] `docs/SPEC.md` non-goals state that competing with general-purpose cleaners
      on global tool caches and `node_modules` is not an objective.
- [x] `docs/CATEGORY.md` distinguishes agent state stores, generic build debris,
      and installed content that is never debris.
- [x] The age-semantics asymmetry is documented — meaningful for session
      transcripts whose mtime is a fixed session end time, structurally broken
      for global caches whose mtime tracks continuous use.
- [x] `ROADMAP.md` reflects the coverage milestone.
- [x] No claim implies coverage the audit does not support.
