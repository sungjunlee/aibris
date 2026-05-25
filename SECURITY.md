# Security Policy

## Reporting a Vulnerability

Please report security vulnerabilities via email to the maintainer.
Do not open a public issue.

## Scope

aibris is a local disk cleanup tool. The primary security concerns are:

1. **Accidental deletion** — The tool deletes files. Safety gates (--dry-run, --interactive, confirmation prompts, --risky flag) are the primary defense.
2. **Path traversal** — All scanned paths are constructed from well-known locations under `$HOME`. No user-provided paths are accepted.
3. **Symlink safety** — Go's `os.RemoveAll` removes symlinks without following them.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.2.x   | Yes       |
| 0.1.x   | No        |
