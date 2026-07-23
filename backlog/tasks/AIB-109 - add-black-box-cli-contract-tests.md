---
id: AIB-109
title: Add black-box CLI contract tests
status: To Do
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

- [ ] Tests build or invoke a real binary in an isolated temporary HOME.
- [ ] Tests cover invalid flags, invalid selectors, invalid roots, dry-run, cancellation, and cleanup execution failure.
- [ ] Tests assert exit status separately from stdout and stderr.
- [ ] No test reads or mutates the developer real HOME.
- [ ] The suite runs on macOS and Linux CI without flaky timing assumptions.
- [ ] Runtime remains acceptable for normal pull-request CI.
- [ ] go test ./..., go build ./..., and go vet ./... pass.
