---
id: AIB-109
title: Add black-box CLI contract tests
status: In Progress
labels:
  - enhancement
  - devops
  - cli
  - type:chore
priority: high
milestone: '0.8.x Reliability & Trust'
created_date: '2026-07-22'
---
## Description
## Goal

Test the compiled aibris process rather than only invoking Cobra commands in-process, so stdout, stderr, prompts, signals, and exit status are covered as user-visible contracts.

## Acceptance criteria

- [x] Tests build or invoke a real binary in an isolated temporary HOME.
- [x] Tests cover invalid flags, invalid selectors, invalid roots, dry-run, cancellation, and cleanup execution failure.
- [x] Tests assert exit status separately from stdout and stderr.
- [x] No test reads or mutates the developer real HOME.
- [x] The suite runs on macOS and Linux CI without flaky timing assumptions.
- [x] Runtime remains acceptable for normal pull-request CI.
- [x] go test ./..., go build ./..., and go vet ./... pass.
