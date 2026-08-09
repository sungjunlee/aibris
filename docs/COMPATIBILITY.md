# 0.x Compatibility and Deprecation Policy

aibris intentionally remains in the 0.x series until the maintainer judges it
ready to change that posture. This policy makes the documented contracts useful
to automation while that work continues. It does not promise a v1.0 scope,
date, or release schedule.

## Stable documented surfaces

During 0.x, aibris treats the following documented surfaces as stable contracts:

- Public command names (`scan` and `clean`) and the root `--help` and
  `--version` behavior.
- Flag names, documented short aliases, accepted selector values, defaults,
  and safety meanings described by `aibris --help` and [SPEC.md](SPEC.md).
- The field names, types, meanings, documented enum values, and exit behavior
  of `scan --json`, `clean --dry-run --json` (`clean_plan`), and execution
  `clean --json` (`clean_receipt`) described in [JSON_SCHEMA.md](JSON_SCHEMA.md).
- Process exit status: successful completion exits `0`; invalid usage, partial
  scan results, fail-closed safety refusals, cancellation, and an execution
  receipt whose status is not `succeeded` exit `1`.

Human-oriented formatting, progress output, internal Go packages, cache-file
layouts, undocumented environment variables, and the filesystem-dependent set
or order of discovered debris are not compatibility surfaces. They may change
without a migration promise.

## Changes and migration notes

A change is breaking during 0.x when it removes or renames a stable flag or
alias, changes a documented default or selector/safety meaning, changes the
name, type, requiredness, or meaning of an existing JSON field or documented
enum value, or changes the documented success/failure exit behavior.

Every breaking change must include all of the following before release:

1. A `CHANGELOG.md` entry that names the affected stable surface.
2. Curated release notes with an **Upgrade and migration** section describing
   the before and after invocation or consumer behavior.
3. A compatibility-impact entry using the release-note template, including a
   replacement for any removed surface.

Compatible additions should preserve existing values and behavior. Consumers
that receive a newer unknown `schema_version` must stop rather than assume the
document shape.

## JSON schema versions

`schema_version` is the machine-readable compatibility boundary. A breaking
JSON change requires a new schema version and the migration notes above. A
consumer must fail closed on a newer unknown version. New optional fields may
be added only when existing documented fields keep their type and meaning;
consumers should ignore fields they do not use within a schema version.

The historical `worktrees` array is a special compatibility alias: it mirrors
the canonical `items` array exactly and is retained throughout 0.x. New
consumers should read `items`.

## Deprecation window

Before removing a documented flag or alias, aibris announces its deprecation in
`--help`, `CHANGELOG.md`, and curated release notes. The announcement names the
replacement and earliest removal version or date. The deprecated surface stays
supported for at least **two subsequent 0.x feature/minor releases and 90
calendar days, whichever is longer**. A later removal remains a breaking
change and therefore needs migration notes.

This minimum does not shorten stronger existing commitments, including the
`worktrees` alias retained for all of 0.x.
