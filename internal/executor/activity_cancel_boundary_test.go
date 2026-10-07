package executor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
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
				path = testutil.GoBuildCache(home)
				item = types.DebrisInfo{Path: path, Category: types.CategoryBuildCache, Tool: types.ToolBuildCache, ID: "go-build", CleanupKind: types.CleanupCommand, CleanupCommand: []string{"go", "clean", "-cache"}}
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
			prepared := PrepareExecutionWithSafety(context.Background(), selection, runtime)
			receipt, err := ExecutePreparedTargets(ctx, prepared, ExecutionOptions{ReceiptKeyFn: func(item types.DebrisInfo) string { return item.Path }}, nil)
			if !errors.Is(err, context.Canceled) || len(receipt.Units) != 1 {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			unit := receipt.Units[0]
			if unit.State != ExecutionCancelled || unit.MutationAttempted || unit.PhysicalRemoved || unit.FreedBytes != 0 || receipt.FreedBytes != 0 || unit.ResidualBytes != 4 {
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

func TestPreparedCancellationAfterCompletedMutationPreservesBatchReceipt(t *testing.T) {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	evidence := cleaner.OverlapSafetyEvidence{Complete: true}
	runtime := cleaner.NewCleanupOverlapSafetyRuntime(evidence, func(context.Context) (cleaner.OverlapSafetyEvidence, error) { return evidence, nil }, nil)
	selection, err := cleaner.ApplyCleanupOverlapSafety(context.Background(), runtime, items)
	if err != nil {
		t.Fatal(err)
	}
	prepared := PrepareExecutionWithSafety(context.Background(), selection, runtime)
	receipt, err := ExecutePreparedTargets(ctx, prepared, ExecutionOptions{Output: cancelOnRemovalWriter{cancel}, ErrorOutput: io.Discard, ReceiptKeyFn: func(item types.DebrisInfo) string { return item.Path }}, nil)
	if !errors.Is(err, context.Canceled) || len(receipt.Units) != 2 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	first, second := receipt.Units[0], receipt.Units[1]
	if first.State != ExecutionRemoved || !first.MutationAttempted || !first.PhysicalRemoved || first.FreedBytes != 4 || first.ResidualBytes != 0 || receipt.FreedBytes != 4 {
		t.Errorf("completed mutation receipt=%+v", first)
	}
	if second.State != ExecutionCancelled || second.MutationAttempted || second.PhysicalRemoved || second.FreedBytes != 0 {
		t.Errorf("remaining receipt=%+v", second)
	}
	if _, err := os.Stat(first.Target.Path); !os.IsNotExist(err) {
		t.Errorf("first target still exists: %v", err)
	}
	if _, err := os.Stat(second.Target.Path); err != nil {
		t.Errorf("second target lost: %v", err)
	}
}
