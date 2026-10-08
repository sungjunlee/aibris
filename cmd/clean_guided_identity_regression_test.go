package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/safedelete"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// Exercise preparation, the production executor/confirmation loop and the
// actual receipt writer. Its os.Exit behavior is checked in a child process.
func TestGuidedReceiptPreservesDeletedTargets(t *testing.T) {
	args := os.Args
	if len(args) >= 4 && args[len(args)-3] == "guided-identity-regression" {
		mode, home := args[len(args)-2], args[len(args)-1]
		resetCleanFlags()
		testutil.SetHome(t, home)
		cleanIncludePaths = true
		realClaude := filepath.Join(home, "dotfiles", "claude")
		if err := os.MkdirAll(realClaude, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realClaude, filepath.Join(home, ".claude")); err != nil {
			t.Fatal(err)
		}
		items := []types.DebrisInfo{
			{Path: filepath.Join(home, "a", "node_modules"), ID: "a", Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8},
			{Path: filepath.Join(home, ".claude", "b", "node_modules"), ID: "b", Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8},
		}
		for _, item := range items {
			writeJSONReceiptFixture(t, item.Path, "payload!")
		}
		plan, _, _, prepared := preparedReceiptFixture(t, items, staticOverlapSafetyRuntime(nil, nil))
		pending, err := newGuidedCleanExecutionReceipt(scanSource{Kind: scanSourceLive}, types.PruneOptions{}, nil, plan, cleanAudit{}, items, nil, prepared)
		if err != nil {
			t.Fatal(err)
		}
		preparedBKey := cleanJSONReceiptItemKey(items[1])
		if strings.HasPrefix(mode, "vanished") {
			// Simulate another process deleting B while confirmation is pending.
			if err := safedelete.RemoveAllUnderHome(items[1].Path); err != nil {
				t.Fatal(err)
			}
		}
		var execution cleanExecutionReceipt
		var executionErr error
		if mode == "vanished-declined" {
			execution, executionErr = interactiveCleanWithValidationAndObserver(context.Background(), confirminput.NewReader(strings.NewReader("y\nn\n")), os.Stdout, prepared, nil, guidedCleanSkipObserver(&pending))
		} else {
			execution, executionErr = executePreparedCleanTargets(context.Background(), prepared, quietActiveWorktreeExecutionOptions())
		}
		if mode == "vanished" && execution.Units[1].ReceiptTargetKey != preparedBKey {
			t.Fatalf("B lost its prepared identity: got=%q want=%q", execution.Units[1].ReceiptTargetKey, preparedBKey)
		}
		switch mode {
		case "missing-key":
			execution.Units[1].ReceiptTargetKey = ""
		case "unknown-key":
			execution.Units[1].ReceiptTargetKey = "unknown"
		case "duplicate":
			execution.Units = append(execution.Units, execution.Units[1])
		case "missing-outcome":
			execution.Units = execution.Units[:1]
		}
		if !execution.Units[0].PhysicalRemoved || execution.Units[0].FreedBytes != 8 {
			t.Fatalf("A was not really removed: %+v", execution)
		}
		cleanReceiptFile = filepath.Join(home, "receipt.json")
		writeGuidedCleanExecutionReceipt(&pending, execution, executionErr)
		if executionErr != nil {
			os.Exit(1)
		}
		return
	}
	for _, mode := range []string{"vanished", "vanished-declined", "missing-key", "unknown-key", "duplicate", "missing-outcome"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			// Skip on hosts where directory symlinks cannot be created.
			probe := filepath.Join(home, "probe")
			if err := os.Symlink(home, probe); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestGuidedReceiptPreservesDeletedTargets$", "--", "guided-identity-regression", mode, home)
			output, err := command.CombinedOutput()
			wantExit := 1
			if mode == "vanished-declined" {
				wantExit = 0
			}
			gotExit := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				gotExit = exit.ExitCode()
			}
			if gotExit != wantExit {
				t.Errorf("exit=%d want=%d output=%s", gotExit, wantExit, output)
			}
			contents, err := os.ReadFile(filepath.Join(home, "receipt.json"))
			if err != nil {
				t.Fatalf("receipt not emitted after deleting A: %v; output=%s", err, output)
			}
			var receipt cleanJSONReceipt
			if err := json.Unmarshal(contents, &receipt); err != nil {
				t.Fatal(err)
			}
			wantRequested := 2
			if mode == "vanished-declined" {
				wantRequested = 1
			}
			if receipt.SchemaVersion != 1 || receipt.Totals.Requested != wantRequested || receipt.Totals.FreedBytes < 8 {
				t.Fatalf("receipt lost accounting: %+v", receipt)
			}
			var a, b *cleanJSONReceiptPhysicalTarget
			for i := range receipt.PhysicalTargets {
				target := &receipt.PhysicalTargets[i]
				if target.Path == nil {
					t.Fatal("path missing")
				}
				if strings.Contains(*target.Path, string(filepath.Separator)+"a"+string(filepath.Separator)) {
					a = target
				} else {
					b = target
				}
			}
			if a == nil || a.State != "removed" || !a.PhysicalRemoved || a.FreedBytes != 8 {
				t.Fatalf("A removal lost: %+v", receipt)
			}
			if b == nil {
				t.Fatal("B missing")
			}
			if _, err := os.Lstat(filepath.Join(home, "a", "node_modules")); !os.IsNotExist(err) {
				t.Fatalf("A was not deleted: %v", err)
			}
			if mode == "vanished" && strings.Contains(string(output), "execution receipt invariant") {
				t.Fatalf("path disappearance changed receipt identity: %s", output)
			}
			if strings.HasPrefix(mode, "vanished") {
				if mode == "vanished" && (receipt.Status != cleanJSONReceiptPartialFailure || b.State != "failed" || !b.PhysicalRemoved || b.FreedBytes != 0 || slices.Contains(b.ReasonCodes, "execution_not_recorded")) {
					t.Fatalf("safety refusal lost: %+v", receipt)
				}
				if mode == "vanished-declined" && receipt.Status != cleanJSONReceiptSucceeded {
					t.Fatalf("normal path disappearance became an invariant error: %+v", receipt)
				}
				if mode == "vanished-declined" && (b.State != "skipped" || b.Requested || !slices.Contains(b.ReasonCodes, "not_confirmed")) {
					t.Fatalf("declined B lost: %+v", b)
				}
			} else {
				if receipt.Status != cleanJSONReceiptPartialFailure || b.State != "failed" || !b.Requested {
					t.Fatalf("identity failure not recorded: %+v", receipt)
				}
				code := "execution_not_recorded"
				if mode == "duplicate" {
					code = "execution_identity_invalid"
				}
				if !slices.Contains(b.ReasonCodes, code) {
					t.Fatalf("identity failure reason missing: %+v", b)
				}
				if mode == "duplicate" && (!b.PhysicalRemoved || b.FreedBytes != 8) {
					t.Fatalf("duplicate lost known mutation: %+v", b)
				}
			}
		})
	}
}
