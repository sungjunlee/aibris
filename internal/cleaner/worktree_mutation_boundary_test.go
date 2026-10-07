package cleaner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestOrphanedWorktreeRechecksAfterPreMutationMeasurement(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, "worktrees", "orphan")
	gitdir := filepath.Join(home, "restored-repository", ".git", "worktrees", "orphan")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owner, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(owner, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	measure := observedSize
	t.Cleanup(func() { observedSize = measure })
	restored := false
	// Restore external Git evidence after the actual pre-removal size walk.
	// This seam accepts either a repeated barrier or one placed after sizing.
	observedSize = func(ctx context.Context, path string) int64 {
		size := measure(ctx, path)
		if !restored {
			restored = true
			if err := os.MkdirAll(gitdir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return size
	}
	attempted := false
	freed, err := ExecuteWithContextAndBarrierWithOutputAndObserver(ctx, []types.DebrisInfo{{
		Path: owner, Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned,
	}}, func(ctx context.Context, item types.DebrisInfo) error {
		if err := adapter.RequireOrphanedWorktreeOwner(ctx, item.Path); err != nil {
			return err
		}
		return nil
	}, io.Discard, io.Discard, func(outcome CleanupMutationOutcome) { attempted = attempted || outcome.MutationAttempted })
	if err == nil || freed != 0 || attempted {
		t.Fatalf("post-barrier Git drift: err=%v freed=%d attempted=%v; want refusal before mutation", err, freed, attempted)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel lost: %v", err)
	}
}
