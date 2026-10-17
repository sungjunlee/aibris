package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestExecuteActiveWorktreeRefusesProtectedLinkRetarget(t *testing.T) {
	home, _, tree := newExecutorWorktree(t, "protected-retarget")
	testutil.SetHome(t, home)
	link := filepath.Join(home, "Documents")
	if err := os.Symlink(filepath.Join(home, "elsewhere"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	item := executorWorktreeItem(tree, 321)
	selected := buildExecutorUnit(t, item)
	opts := fixtureExecutionOptions(home)
	calls := 0
	opts.BeforeMutation = func(context.Context) error {
		calls++
		if calls == 2 {
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tree, link); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, opts)
	if err == nil || result.PhysicalRemoved || calls != 2 {
		t.Errorf("execution = %+v, %v, calls=%d; want refusal", result, err, calls)
	}
	if data, err := os.ReadFile(filepath.Join(tree, "README.md")); err != nil || string(data) != "initial\n" {
		t.Fatalf("tracked sentinel changed: %q, %v", data, err)
	}
}
