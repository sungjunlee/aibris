package adapter

import "testing"

func TestIsWorktreeSidecarName(t *testing.T) {
	if !IsWorktreeSidecarName(".orca-worktree-trash") {
		t.Fatal("expected .orca-worktree-trash to be registered")
	}
	if IsWorktreeSidecarName(".trash") {
		t.Fatal("unknown names must not be sidecars")
	}
}
