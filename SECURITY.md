# Security Policy

## Reporting a Vulnerability

Please report suspected security vulnerabilities privately through GitHub:
<https://github.com/sungjunlee/aibris/security/advisories/new>. Do not open a
public issue for an unpatched vulnerability.

You can expect an acknowledgement within 7 days and a status update (fix
plan, or why it is not treated as a vulnerability) within 30 days.

Include as much detail as possible:

- aibris version and install method
- operating system
- exact command or workflow involved
- reproduction steps
- whether the issue involves deletion boundaries, symlinks, path validation,
  risky categories, or release/install integrity

## Scope

aibris is a local disk cleanup tool. Its primary security concerns are:

1. **Accidental deletion** — The tool deletes files. `--dry-run`, `--interactive`, confirmation prompts, age gates, and `--risky` are the primary defenses.
2. **Path boundaries** — Cleanup targets come from known locations under `$HOME`; arbitrary user-provided paths are not accepted, and every deletion passes one gate (`internal/safedelete`) that refuses protected locations, their ancestors, primary Git repositories, and Git metadata.
3. **Symlink handling** — Safety checks resolve symlinks when possible before deletion.

See [SECURITY_AUDIT.md](docs/SECURITY_AUDIT.md) for the current safety model,
destructive-operation boundaries, known limitations, and release integrity
signals.

## What We Consider Security-Relevant

- deletion outside intended cleanup boundaries
- path validation bypasses
- unsafe symlink handling
- risky AI logs being deleted without `--risky`
- release artifact or installer integrity issues
- logic bugs that can cause unintended destructive behavior

Cleanup misses, false negatives, cosmetic output issues, and requests for more
aggressive cleanup are usually normal bugs or feature requests.

## Supported Versions

Security fixes are provided for the latest released 0.x minor line. Because the
project is pre-1.0, older minor lines do not receive backports. Upgrade to the
latest release before reporting or validating a fix.

| Version  | Supported |
|----------|-----------|
| 0.12.x   | Yes       |
| <=0.11.x | No        |
