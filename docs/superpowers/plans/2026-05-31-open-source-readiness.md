# Open Source Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Prepare aibris for a credible initial open-source release as a focused AI-work-debris cleanup CLI.

**Architecture:** Keep the CLI small and conservative: adapters discover known debris, scanner aggregates, cleaner filters and deletes. This pass improves trust signals first: deterministic tests, current documentation, and explicit safety boundaries.

**Tech Stack:** Go 1.26, Cobra, Go test/vet, GitHub Actions, GoReleaser.

---

### Task 1: Isolate Default Scanner Test

**Files:**
- Modify: `internal/scanner/scanner_test.go`

- [x] **Step 1: Reproduce the failing test**

Run:

```bash
go test ./... -run '^(TestScan_Default)$' -count=1 -timeout=3s
```

Expected: FAIL in `TestScan_Default` because the default scanner walks real `$HOME` caches.

- [x] **Step 2: Make the default scanner test use an empty home**

Edit `TestScan_Default`:

```go
func TestScan_Default(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	result, err := Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
```

- [x] **Step 3: Verify the targeted test passes**

Run:

```bash
go test ./... -run '^(TestScan_Default)$' -count=1 -timeout=10s
```

Expected: PASS.

### Task 2: Align Public Documentation With Current CLI

**Files:**
- Modify: `README.md`
- Modify: `docs/SPEC.md`

- [x] **Step 1: Update README positioning**

Keep the first screen focused on AI coding debris, not generic Mac cleanup:

```markdown
Scan and clean disk debris left by AI coding workflows: temporary worktrees,
tool logs, dependency folders, and build caches that accumulate while agents
branch, build, test, and retry.
```

- [x] **Step 2: Add a concrete scan and dry-run example**

Add terminal examples showing `aibris scan` grouping, total size, and `aibris clean --dry-run`.

- [x] **Step 3: Replace stale SPEC references**

Update `docs/SPEC.md` so it describes `clean`, categories, `--risky`, JSON output, and the current supported adapters. Remove stale `prune`, `--all`, and Cursor/Windsurf non-goal statements.

### Task 3: Add Safety Audit Document

**Files:**
- Create: `SECURITY_AUDIT.md`
- Modify: `SECURITY.md`

- [x] **Step 1: Document destructive-operation boundaries**

Create `SECURITY_AUDIT.md` with sections:

```markdown
# aibris Security Audit

## Executive Summary
## Threat Surface
## Destructive Operation Boundaries
## Path and Symlink Handling
## Risky Categories
## Dry-Run and Confirmation Controls
## Release Integrity
## Testing Coverage
## Known Limitations
```

- [x] **Step 2: Link it from SECURITY.md**

Add a short safety model summary and a link to `SECURITY_AUDIT.md`.

### Task 4: Verification

**Files:**
- No source changes unless verification exposes a defect.

- [x] **Step 1: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [x] **Step 2: Run build**

Run:

```bash
go build ./...
```

Expected: PASS.

- [x] **Step 3: Run static analysis**

Run:

```bash
go vet ./...
```

Expected: PASS.

- [x] **Step 4: Inspect diff**

Run:

```bash
git diff --stat
git diff --check
```

Expected: only intended files changed; no whitespace errors.
