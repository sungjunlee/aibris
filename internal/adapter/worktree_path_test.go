package adapter

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorktreePathIdentityPreservesSpelling(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "KeepCase")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolved = filepath.Clean(resolved)
	if got := resolvedExistingPath(dir); got != resolved {
		t.Fatalf("resolvedExistingPath = %q; want %q", got, resolved)
	}
	lower := strings.ToLower(resolved)
	if runtime.GOOS == "windows" {
		if got := canonicalExistingPath(dir); got != lower {
			t.Fatalf("canonicalExistingPath = %q; want %q", got, lower)
		}
		if !sameCleanPath(resolved, lower) {
			t.Fatal("windows lexical identity must ignore case and must not resolve symlinks")
		}
	} else {
		if got := canonicalExistingPath(dir); got != resolved {
			t.Fatalf("canonicalExistingPath = %q; want %q", got, resolved)
		}
		if resolved != lower && sameCleanPath(resolved, lower) {
			t.Fatal("non-windows lexical identity must stay case-sensitive")
		}
	}
	missing := filepath.Join(dir, "missing")
	if got := resolvedExistingPath(missing); got != filepath.Clean(missing) {
		t.Fatalf("missing resolvedExistingPath = %q; want cleaned input", got)
	}
}
