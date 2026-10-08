package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func guidedIdentityFixture(t *testing.T) (UnifiedCleanupPlan, []types.DebrisInfo, []preparedCleanTarget) {
	t.Helper()
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	item := types.DebrisInfo{Path: filepath.Join(home, "project", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8}
	writeJSONReceiptFixture(t, item.Path, "payload!")
	plan, _, _, prepared := preparedReceiptFixture(t, []types.DebrisInfo{item}, staticOverlapSafetyRuntime(nil, nil))
	// Exercise the production executor without permitting any cleanup.
	prepared[0].PreparationError = errors.New("fixture refuses mutation")
	return plan, []types.DebrisInfo{item}, prepared
}

func TestGuidedReceiptProductionRejectsPreparedIdentity(t *testing.T) {
	for _, name := range []string{"duplicate", "duplicate physical identity", "duplicate physical component", "missing"} {
		t.Run(name, func(t *testing.T) {
			plan, items, prepared := guidedIdentityFixture(t)
			switch name {
			case "duplicate":
				prepared = append(prepared, prepared[0])
			case "duplicate physical identity":
				other := prepared[0]
				other.Item.ID = "other-logical-row"
				prepared = append(prepared, other)
			case "duplicate physical component":
				plan.Components = append(plan.Components, plan.Components[0])
			case "missing":
				prepared[0].Item.Path = filepath.Join(t.TempDir(), "unplanned", "node_modules")
			}
			_, err := newGuidedCleanExecutionReceipt(scanSource{Kind: scanSourceLive}, types.PruneOptions{}, nil, plan, cleanAudit{}, items, nil, prepared)
			if err == nil || !strings.Contains(err.Error(), "execution receipt invariant") {
				t.Fatalf("invalid prepared identity accepted: %v", err)
			}
			if _, err := os.Lstat(items[0].Path); err != nil {
				t.Fatalf("receipt preparation mutated target: %v", err)
			}
		})
	}
}

func TestGuidedReceiptProductionRejectsExecutionIdentity(t *testing.T) {
	for _, name := range []string{"missing key", "unknown key", "duplicate", "missing outcome", "duplicate skip", "skip and execution", "unknown skip"} {
		t.Run(name, func(t *testing.T) {
			plan, items, prepared := guidedIdentityFixture(t)
			pending, err := newGuidedCleanExecutionReceipt(scanSource{Kind: scanSourceLive}, types.PruneOptions{}, nil, plan, cleanAudit{}, items, nil, prepared)
			if err != nil {
				t.Fatal(err)
			}
			execution, executionErr := executePreparedCleanTargets(context.Background(), prepared, quietActiveWorktreeExecutionOptions())
			if executionErr == nil || len(execution.Units) != 1 {
				t.Fatalf("expected production preparation refusal: %+v, %v", execution, executionErr)
			}
			skip := interactiveCleanSkipOutcome{Target: prepared[0], Declined: true}
			switch name {
			case "missing key":
				execution.Units[0].ReceiptTargetKey = ""
			case "unknown key":
				execution.Units[0].ReceiptTargetKey = "unknown"
			case "duplicate":
				execution.Units = append(execution.Units, execution.Units[0])
			case "missing outcome":
				execution.Units = nil
			case "duplicate skip":
				pending.observeInteractiveSkip(skip)
				pending.observeInteractiveSkip(skip)
				execution.Units = nil
			case "skip and execution":
				pending.observeInteractiveSkip(skip)
			case "unknown skip":
				skip.Target.Item.ID = "unknown"
				pending.observeInteractiveSkip(skip)
			}
			receipt, err := pending.finish(execution, executionErr)
			if err == nil || !strings.Contains(err.Error(), "execution receipt invariant") {
				t.Fatalf("invalid execution identity accepted: receipt=%+v error=%v", receipt, err)
			}
			if receipt.SchemaVersion != 0 {
				t.Fatalf("invalid identity yielded an emittable receipt: %+v", receipt)
			}
			if _, err := os.Lstat(items[0].Path); err != nil {
				t.Fatalf("fixture allowed cleanup: %v", err)
			}
		})
	}
}

// Invoke the actual writer in a child process because it reports receipt
// failures with os.Exit. Existing sinks must survive identity failures intact.
func TestGuidedReceiptProductionDoesNotEmitInvalidIdentity(t *testing.T) {
	args := os.Args
	if len(args) >= 4 && args[len(args)-3] == "guided-receipt-child" {
		plan, items, prepared := guidedIdentityFixture(t)
		pending, err := newGuidedCleanExecutionReceipt(scanSource{Kind: scanSourceLive}, types.PruneOptions{}, nil, plan, cleanAudit{}, items, nil, prepared)
		if err != nil {
			t.Fatal(err)
		}
		execution, executionErr := executePreparedCleanTargets(context.Background(), prepared, quietActiveWorktreeExecutionOptions())
		if args[len(args)-2] == "duplicate" {
			execution.Units = append(execution.Units, execution.Units[0])
		} else {
			execution.Units[0].ReceiptTargetKey = ""
		}
		cleanReceiptFile = args[len(args)-1]
		writeGuidedCleanExecutionReceipt(&pending, execution, executionErr)
		t.Fatal("writer returned after invalid identity")
	}
	for _, mode := range []string{"missing", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			sink := filepath.Join(home, "receipt.json")
			const original = "existing receipt\n"
			if err := os.WriteFile(sink, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestGuidedReceiptProductionDoesNotEmitInvalidIdentity$", "--", "guided-receipt-child", mode, sink)
			output, err := command.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "execution receipt invariant") {
				t.Fatalf("writer did not report invariant failure: error=%v output=%s", err, output)
			}
			contents, err := os.ReadFile(sink)
			if err != nil || string(contents) != original {
				t.Fatalf("invalid receipt overwrote sink: contents=%q error=%v", contents, err)
			}
		})
	}
}
