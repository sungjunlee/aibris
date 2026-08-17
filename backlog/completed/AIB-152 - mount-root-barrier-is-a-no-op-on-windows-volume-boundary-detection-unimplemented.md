---
id: AIB-152
title: Mount-root barrier is a no-op on Windows; volume-boundary detection unimplemented
status: To Do
labels:
  - bug
  - scanner
  - safety
  - type:bug
priority: high
milestone: 0.10.x Agent State Store Coverage
created_date: '2026-07-28'
---
## Description
## Problem

The ancestor barrier's mount-root check is Unix-only. On Windows,
`recordedCWDDeviceID` is a no-op that reports "not a mount root", so
classification falls through to home/temp containment alone.

A recorded working directory under a volume mounted at a folder **inside** the
Windows home directory — for example a volume mounted at
`C:\Users\<user>\mnt\data` — is therefore treated as proven deleted when the
volume is not mounted, and the entry becomes `orphaned`: cleanable by default
with no age gate and without `--risky`.

## What is already covered

The realistic cases are handled by the existing home/temp barrier:

- a disconnected drive letter (`Z:\project`) — walking up reaches `Z:\`, which
  does not exist, so absence is never proven;
- a UNC path (`\\server\share\project`) — the nearest existing ancestor falls
  outside the home directory, so containment fails and the entry is
  `undetermined`.

The residual gap is specifically a volume mount point inside the user profile.

## Why it was not implemented in #148

Deliberate, with maintainer sign-off. Detection needs `GetVolumePathNameW`
(reachable in pure stdlib via `syscall.NewLazyDLL`, so no new dependency), but
it cannot be executed on any machine or CI runner this project has. Shipping
unverifiable syscall code inside a safety barrier is its own risk: a wrong
implementation either reduces Windows reclamation to zero, or wrongly proves
absence and deletes recoverable state.

#148 instead added a `GOOS=windows` build step to CI so the compile-break class
is caught, and stated the limitation in the code.

## Acceptance criteria

- [ ] The Windows device/volume lookup identifies a mount point using
      `GetVolumePathNameW`, comparing the volume path of the nearest existing
      ancestor with that of its parent.
- [ ] An API failure is treated as "not a mount root", preserving current
      behavior rather than collapsing every entry to `undetermined`.
- [ ] Verified on an actual Windows host or a Windows CI runner — not by
      inspection alone. Add the runner if none exists.
- [ ] `~/.claude` and `~/.cursor` classification on Unix is unchanged.

Follows #149, which closed the Unix half.

