package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanupMinimumSizeUsesScannedApparentBytes(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	bin := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(filepath.Join(bin, "du"), []byte("#!/bin/sh\nshift\nfor p do printf '0\\t%s\\n' \"$p\"; done\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	deps := filepath.Join(home, "project", "node_modules")
	if err := os.MkdirAll(deps, 0o700); err != nil {
		t.Fatal(err)
	}
	const size = int64(1 << 30)
	f, err := os.Create(filepath.Join(deps, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := (&adapter.NodeModulesAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %v, %v", items, err)
	}
	now := time.Now()
	unit := cleanupPolicyUnit("fixture", now.Add(-30*24*time.Hour), items[0].Size, filepath.Join(home, "repo", ".git"))
	keeper := cleanupPolicyUnit("keeper", now.Add(-10*24*time.Hour), size, filepath.Join(home, "repo", ".git"))
	policy := DefaultCleanupPolicy(now)
	policy.KeepPerRepository = 1
	for _, tt := range []struct {
		threshold int64
		class     DecisionClass
	}{{size, DecisionRecommended}, {size + 1, DecisionReviewable}} {
		policy.MinSize = tt.threshold
		plan := PlanWorktreeCleanup([]WorktreeCleanupUnit{unit, keeper}, policy)
		if got := plan.Decisions[0]; got.Class != tt.class {
			t.Errorf("size %d threshold %d: decision = %v; want %s", items[0].Size, tt.threshold, got, tt.class)
		}
		if tt.class == DecisionReviewable && plan.Decisions[0].Reasons[0].Code != DecisionReasonMinimumSize {
			t.Errorf("expected minimum-size reason: %v", plan.Decisions[0].Reasons)
		}
	}
}
