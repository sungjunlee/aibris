---
id: AIB-122
title: Curate release notes and automate public-doc link checks
status: To Do
labels:
  - documentation
  - docs
  - devops
  - area:docs
  - area:oss
  - type:chore
  - area:release
priority: high
milestone: '0.x OSS Distribution & Release Trust'
created_date: '2026-07-22'
---
## Description
## Goal

Make each release explain user impact and prevent stale or broken public links.

## Acceptance criteria

- [ ] GitHub Release notes are sourced from curated changelog sections rather than an unfiltered commit list.
- [ ] Backlog-only commit noise is excluded from public notes.
- [ ] CI checks repository-local Markdown links and configured community links.
- [ ] Version examples in active installation docs are current or intentionally generic.
- [ ] Release notes always include upgrade, compatibility, and safety notes when relevant.
