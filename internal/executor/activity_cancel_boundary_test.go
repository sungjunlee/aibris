package executor_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/executor"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPreparedCancellationBeforeMutationMatchesReceipt(t *testing.T) {
	for _, command := range []bool{false, true} {
		name := "path"
		if command {
			name = "command"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, "project", "node_modules")
			item := types.DebrisInfo{Path: path, Category: types.CategoryNodeModules, Tool: types.ToolNodeModules}
			if command {
				path = testutil.UVCache(home)
				item = types.DebrisInfo{Path: path, Category: types.CategoryOtherCache, Tool: types.ToolPipCache, ID: "uv", CleanupKind: types.CleanupCommand, CleanupCommand: []string{"uv", "cache", "clean"}}
			}
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "keep"), []byte("data"), 0o600); err != nil {
				t.Fatal(err)
			}
			// No real package-manager binary can be invoked, including on failure.
			t.Setenv("PATH", t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			evidence := cleaner.OverlapSafetyEvidence{Complete: true}
			runtime := cleaner.NewCleanupOverlapSafetyRuntime(evidence, func(context.Context) (cleaner.OverlapSafetyEvidence, error) { cancel(); return evidence, nil }, nil)
			selection, err := cleaner.ApplyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{item})
			if err != nil {
				t.Fatal(err)
			}
			prepared := executor.PrepareExecutionWithSafety(context.Background(), selection, runtime)
			receipt, err := executor.ExecutePreparedTargets(ctx, prepared, executor.ExecutionOptions{}, nil)
			if !errors.Is(err, context.Canceled) || len(receipt.Units) != 1 {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			unit := receipt.Units[0]
			if unit.State != executor.ExecutionCancelled || unit.MutationAttempted || unit.PhysicalRemoved || unit.FreedBytes != 0 || receipt.FreedBytes != 0 || unit.ResidualBytes != 4 {
				t.Errorf("pre-mutation cancellation receipt mismatches retained target: %+v", unit)
			}
			if _, err := os.Stat(filepath.Join(path, "keep")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type cancelOnRemovalWriter struct{ cancel context.CancelFunc }

func (w cancelOnRemovalWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "removed:") {
		w.cancel()
	}
	return len(p), nil
}

// Cancel on the first context check after the target disappears, before
// reclamation returns. This models cancellation during a successful removal
// without relying on filesystem speed or cancelling from the final log line.
type cancelOnDisappearanceContext struct {
	context.Context
	path   string
	cancel context.CancelFunc
}

func (c cancelOnDisappearanceContext) Err() error {
	if _, err := os.Lstat(c.path); os.IsNotExist(err) {
		c.cancel()
	}
	return c.Context.Err()
}

func TestPreparedCancellationAfterCompletedMutationPreservesBatchReceipt(t *testing.T) {
	for _, phase := range []string{"before reclamation returns", "after removed log"} {
		t.Run(phase, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			var items []types.DebrisInfo
			for _, name := range []string{"a", "b"} {
				path := filepath.Join(home, name, "node_modules")
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "payload"), []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
				items = append(items, types.DebrisInfo{Path: path, Category: types.CategoryNodeModules, Tool: types.ToolNodeModules})
			}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			var ctx context.Context = base
			var output io.Writer = cancelOnRemovalWriter{cancel}
			if phase == "before reclamation returns" {
				ctx = cancelOnDisappearanceContext{base, items[0].Path, cancel}
				output = io.Discard
			}
			evidence := cleaner.OverlapSafetyEvidence{Complete: true}
			runtime := cleaner.NewCleanupOverlapSafetyRuntime(evidence, func(context.Context) (cleaner.OverlapSafetyEvidence, error) { return evidence, nil }, nil)
			selection, err := cleaner.ApplyCleanupOverlapSafety(context.Background(), runtime, items)
			if err != nil {
				t.Fatal(err)
			}
			prepared := executor.PrepareExecutionWithSafety(context.Background(), selection, runtime)
			receipt, err := executor.ExecutePreparedTargets(ctx, prepared, executor.ExecutionOptions{Output: output, ErrorOutput: io.Discard}, nil)
			if !errors.Is(err, context.Canceled) || len(receipt.Units) != 2 {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			first, second := receipt.Units[0], receipt.Units[1]
			if first.State != executor.ExecutionRemoved || !first.MutationAttempted || !first.PhysicalRemoved || first.FreedBytes != 4 || first.ResidualBytes != 0 || receipt.FreedBytes != 4 || first.Error != "" {
				t.Errorf("completed mutation receipt=%+v", first)
			}
			if second.State != executor.ExecutionCancelled || second.MutationAttempted || second.PhysicalRemoved || second.FreedBytes != 0 {
				t.Errorf("remaining receipt=%+v", second)
			}
			if _, err := os.Stat(first.Target.Path); !os.IsNotExist(err) {
				t.Errorf("first target still exists: %v", err)
			}
			if _, err := os.Stat(second.Target.Path); err != nil {
				t.Errorf("second target lost: %v", err)
			}
		})
	}
}

func TestPreparedPartialRemovalWithUnreadableSiblingPreservesReceipt(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, "project", "node_modules")
	unreadable := filepath.Join(path, "a")
	removable := filepath.Join(path, "b")
	for _, dir := range []string{unreadable, removable} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(unreadable, "keep"), make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(removable, "payload"), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(unreadable, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(unreadable); !os.IsPermission(err) {
		t.Skipf("fixture requires enforced directory permissions (e.g. non-root Unix): %v", err)
	}
	item := types.DebrisInfo{Path: path, Category: types.CategoryNodeModules, Tool: types.ToolNodeModules}
	ctx := context.Background()
	evidence := cleaner.OverlapSafetyEvidence{Complete: true}
	runtime := cleaner.NewCleanupOverlapSafetyRuntime(evidence, func(context.Context) (cleaner.OverlapSafetyEvidence, error) { return evidence, nil }, nil)
	selection, err := cleaner.ApplyCleanupOverlapSafety(ctx, runtime, []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	prepared := executor.PrepareExecutionWithSafety(ctx, selection, runtime)
	receipt, err := executor.ExecutePreparedTargets(ctx, prepared, executor.ExecutionOptions{Output: io.Discard, ErrorOutput: io.Discard}, nil)
	if !errors.Is(err, os.ErrPermission) || len(receipt.Units) != 1 {
		t.Fatalf("receipt=%+v err=%v; want one unit and a permission error", receipt, err)
	}
	unit := receipt.Units[0]
	// The unreadable bytes are absent from both approximate measurements.
	if unit.State != executor.ExecutionPartial || !unit.MutationAttempted || unit.PhysicalRemoved || unit.FreedBytes != 1000 || unit.ResidualBytes != 0 || receipt.FreedBytes != 1000 || !errors.Is(unit.FailureCause, os.ErrPermission) {
		t.Errorf("partial removal receipt=%+v batch freed=%d", unit, receipt.FreedBytes)
	}
	if reasons := cleanjson.CleanJSONReceiptStateReasons(string(unit.State), unit.PhysicalRemoved, unit.FreedBytes, unit.CommandFallbackPathRemoval, unit.FailureCause, func(err error) bool { return errors.Is(err, cleaner.ErrCleanupTargetYoungerThanMinimumAge) }); !slices.Contains(reasons, "partial_failure") {
		t.Errorf("partial removal JSON reasons=%v; want partial_failure", reasons)
	}
	if _, err := os.Stat(removable); !os.IsNotExist(err) {
		t.Errorf("removable sibling survived: %v", err)
	}
	if err := os.Chmod(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(unreadable, "keep")); err != nil || len(data) != 10 {
		t.Errorf("unreadable sibling lost: bytes=%d err=%v", len(data), err)
	}
}
