package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathStrictlyWithin(t *testing.T) {
	base := t.TempDir() // absolute on every OS, including a Windows drive
	root := filepath.Join(base, "unit")
	tests := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "node_modules"), true},
		{filepath.Join(root, "a", "b"), true},
		{root, false},
		{filepath.Join(root, "..", "sibling", "node_modules"), false},
		{filepath.Join(base, "unit-other", "node_modules"), false},
		{"relative/node_modules", false},
	}
	for _, tt := range tests {
		if got := pathStrictlyWithin(root, tt.path); got != tt.want {
			t.Errorf("pathStrictlyWithin(%q, %q) = %t; want %t", root, tt.path, got, tt.want)
		}
	}
}

func TestStripPathHasNoSymlinksRefusesSymlinkedAncestor(t *testing.T) {
	unit := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(unit, "android")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := stripPathHasNoSymlinks(unit, filepath.Join(unit, "android", "build")); got != "symlink on the path to the subtree" {
		t.Fatalf("reason = %q; want symlink refusal", got)
	}
	if err := os.MkdirAll(filepath.Join(unit, "ios", "Pods"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := stripPathHasNoSymlinks(unit, filepath.Join(unit, "ios", "Pods")); got != "" {
		t.Fatalf("real directory refused: %q", got)
	}
}

func TestStripSubtreeUnchangedDetectsReplacement(t *testing.T) {
	unit := t.TempDir()
	subtree := filepath.Join(unit, "node_modules")
	if err := os.Mkdir(subtree, 0o755); err != nil {
		t.Fatal(err)
	}
	authorized, err := os.Lstat(subtree)
	if err != nil {
		t.Fatal(err)
	}
	if got := stripSubtreeUnchanged(unit, subtree, authorized); got != "" {
		t.Fatalf("unchanged subtree refused: %q", got)
	}
	if err := os.Remove(subtree); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), subtree); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := stripSubtreeUnchanged(unit, subtree, authorized); got == "" {
		t.Fatal("subtree swapped for a symlink was not refused")
	}
}
