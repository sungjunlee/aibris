package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestOverlapProvenanceCarriesRowsAndSuccessfulObligationsToReceipt(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outer := filepath.Join(home, ".cache", "provenance-success")
	claudeEntry := filepath.Join(outer, "agent", "claude")
	cursorEntry := filepath.Join(outer, "agent", "cursor")
	for _, path := range []string{claudeEntry, cursorEntry} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	owner := overlapCmdTarget(outer, 1000)
	claude := overlapCmdAgentStateItem(claudeEntry, types.EntryClassOrphaned)
	cursor := overlapCmdAgentStateItem(cursorEntry, types.EntryClassOrphaned)
	cursor.Tool = types.ToolCursor
	logicalInputs := []cleanupOverlapLogicalInput{
		{Item: cursor, PolicyReason: "cursor orphan policy"},
		{Item: claude, PolicyReason: "claude duplicate policy"},
		{Item: owner, PolicyReason: "outer cache policy"},
		{Item: claude, PolicyReason: "claude duplicate policy"},
	}
	var calls []string
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(_ context.Context, path string) (types.EntryClass, error) {
			calls = append(calls, path)
			return types.EntryClassOrphaned, nil
		},
		types.ToolCursor: func(_ context.Context, path string) (types.EntryClass, error) {
			calls = append(calls, path)
			return types.EntryClassOrphaned, nil
		},
	})
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{cursor, claude}, lookup)

	forward, err := applyCleanupOverlapSafetyWithRows(
		context.Background(),
		runtime,
		[]types.DebrisInfo{owner},
		logicalInputs,
	)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := applyCleanupOverlapSafetyWithRows(
		context.Background(),
		runtime,
		[]types.DebrisInfo{owner},
		[]cleanupOverlapLogicalInput{
			logicalInputs[3],
			logicalInputs[2],
			logicalInputs[1],
			logicalInputs[0],
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupComponentLineageEqual(t, forward.Components, reversed.Components)

	if len(forward.Components) != 1 {
		t.Fatalf("components = %d; want one", len(forward.Components))
	}
	component := forward.Components[0]
	if component.Owner.Path != outer || len(component.LogicalRows) != 4 ||
		len(component.Obligations) != 2 {
		t.Fatalf("component = %+v; want owner, four evidence rows, and two obligations", component)
	}
	var physicalBytes int64
	var exactClaudeRows int
	for _, row := range component.LogicalRows {
		physicalBytes += row.PhysicalBytes
		if row.Item.Path == claudeEntry {
			exactClaudeRows++
			if row.PolicyReason != "claude duplicate policy" ||
				!row.RevalidationRequired ||
				row.L1Reason != "nested agent-state revalidation required" {
				t.Fatalf("Claude logical row lost provenance: %+v", row)
			}
		}
	}
	if physicalBytes != 1000 || exactClaudeRows != 2 {
		t.Fatalf("physical bytes/duplicate rows = %d/%d; want 1000/2", physicalBytes, exactClaudeRows)
	}

	prepared := prepareCleanExecutionWithSafety(context.Background(), forward, runtime)
	if len(prepared) != 1 || prepared[0].Component == nil ||
		len(prepared[0].Component.Obligations) != 2 {
		t.Fatalf("prepared targets = %+v; owner-plus-obligations bridge collapsed", prepared)
	}
	receipt, err := executePreparedCleanTargets(
		context.Background(),
		prepared,
		defaultActiveWorktreeExecutionOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || receipt.FreedBytes != 1000 {
		t.Fatalf("revalidation calls/freed = %v/%d; want two and 1000", calls, receipt.FreedBytes)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.Component == nil || !unit.PhysicalRemoved || unit.FreedBytes != 1000 ||
		len(unit.Obligations) != 2 {
		t.Fatalf("unit receipt = %+v; want one removed physical component", unit)
	}
	for _, outcome := range unit.Obligations {
		if outcome.State != cleaner.AgentStateRevalidationPassed ||
			outcome.Classification != types.EntryClassOrphaned ||
			outcome.Reason != "" {
			t.Fatalf("obligation outcome = %+v; want passed orphan", outcome)
		}
	}
	output := captureOutput(func() {
		printWorktreeExecutionReceipts(receipt)
		printGuidedCleanupReceipt(1, receipt)
	})
	for _, want := range []string{
		"cleanup component receipt",
		"physical-removed true   freed 1000 B",
		"obligation passed",
		claudeEntry,
		cursorEntry,
		"targets    1 item",
		"freed      1000 B",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("human receipt missing %q:\n%s", want, output)
		}
	}
}

func TestOverlapProvenanceFailureAndCancellationPreserveCompleteComponent(t *testing.T) {
	t.Run("one child fails after another passes", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home)
		outer := filepath.Join(home, ".cache", "provenance-failure")
		first := filepath.Join(outer, "agent", "a-first")
		second := filepath.Join(outer, "agent", "b-second")
		for _, path := range []string{first, second} {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		failure := errors.New("second obligation failed")
		lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
			types.ToolClaude: func(_ context.Context, path string) (types.EntryClass, error) {
				if filepath.Base(path) == "b-second" {
					return "", failure
				}
				return types.EntryClassOrphaned, nil
			},
		})
		inventory := []types.DebrisInfo{
			overlapCmdAgentStateItem(second, types.EntryClassOrphaned),
			overlapCmdAgentStateItem(first, types.EntryClassOrphaned),
		}
		runtime := staticOverlapSafetyRuntime(inventory, lookup)
		selection, err := applyCleanupOverlapSafety(
			context.Background(),
			runtime,
			[]types.DebrisInfo{overlapCmdTarget(outer, 700)},
		)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if !errors.Is(err, failure) {
			t.Fatalf("error = %v; want child failure", err)
		}
		unit := singleExecutionUnit(t, receipt)
		canonicalSecond, _ := cleaner.TargetPathKey(second)
		if unit.PhysicalRemoved || unit.FreedBytes != 0 || receipt.FreedBytes != 0 ||
			unit.BlockingPath != canonicalSecond || len(unit.Obligations) != 2 {
			t.Fatalf("receipt = %+v; complete component must survive with blocker", receipt)
		}
		if unit.Obligations[0].State != cleaner.AgentStateRevalidationPassed ||
			unit.Obligations[1].State != cleaner.AgentStateRevalidationBlocked ||
			!strings.Contains(unit.Obligations[1].Reason, failure.Error()) {
			t.Fatalf("outcomes = %+v; want passed then blocked", unit.Obligations)
		}
		assertPathExists(t, outer)
	})

	t.Run("component cancellation leaves obligations not attempted", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home)
		outer := filepath.Join(home, ".cache", "provenance-cancel")
		entry := filepath.Join(outer, "agent", "orphan")
		if err := os.MkdirAll(entry, 0o755); err != nil {
			t.Fatal(err)
		}
		lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
			types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
				t.Fatal("revalidator must not run after component cancellation")
				return "", nil
			},
		})
		inventory := []types.DebrisInfo{
			overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
		}
		runtime := staticOverlapSafetyRuntime(inventory, lookup)
		selection, err := applyCleanupOverlapSafety(
			context.Background(),
			runtime,
			[]types.DebrisInfo{overlapCmdTarget(outer, 500)},
		)
		if err != nil {
			t.Fatal(err)
		}
		prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v; want context cancellation", err)
		}
		unit := singleExecutionUnit(t, receipt)
		if unit.PhysicalRemoved || unit.FreedBytes != 0 ||
			unit.BlockingPath != outer ||
			len(unit.Obligations) != 1 ||
			unit.Obligations[0].State != cleaner.AgentStateRevalidationNotAttempted {
			t.Fatalf("cancelled receipt = %+v; want component blocker and not-attempted child", unit)
		}
		output := captureOutput(func() {
			printWorktreeExecutionReceipts(receipt)
		})
		for _, want := range []string{"obligation not-attempted", "blocker", outer} {
			if !strings.Contains(output, want) {
				t.Fatalf("cancelled human receipt missing %q:\n%s", want, output)
			}
		}
		assertPathExists(t, outer)
	})

	for _, classification := range []types.EntryClass{
		types.EntryClassLive,
		types.EntryClassUndetermined,
	} {
		t.Run("classification drift "+string(classification), func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			outer := filepath.Join(home, ".cache", "provenance-drift-"+string(classification))
			entry := filepath.Join(outer, "agent", "orphan")
			if err := os.MkdirAll(entry, 0o755); err != nil {
				t.Fatal(err)
			}
			initial := overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)
			refreshed := initial
			refreshed.Classification = classification
			lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
				types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
					t.Fatal("hard-lock drift must refuse before child revalidation")
					return "", nil
				},
			})
			runtime := cleanupOverlapSafetyRuntime{
				OverlapRuntime: cleaner.OverlapRuntime{
					Initial: cleaner.OverlapSafetyEvidence{
						Items:    []types.DebrisInfo{initial},
						Complete: true,
					},
					Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
						return cleaner.OverlapSafetyEvidence{
							Items:    []types.DebrisInfo{refreshed},
							Complete: true,
						}, nil
					},
					Lookup: lookup,
				},
			}
			selection, err := applyCleanupOverlapSafety(
				context.Background(),
				runtime,
				[]types.DebrisInfo{overlapCmdTarget(outer, 400)},
			)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := executeCleanTargets(context.Background(), selection, runtime)
			if err == nil {
				t.Fatal("classification drift error = nil")
			}
			unit := singleExecutionUnit(t, receipt)
			canonicalEntry, _ := cleaner.TargetPathKey(entry)
			canonicalBlocker, _ := cleaner.TargetPathKey(unit.BlockingPath)
			if unit.PhysicalRemoved || unit.FreedBytes != 0 ||
				canonicalBlocker != canonicalEntry ||
				len(unit.Obligations) != 1 ||
				unit.Obligations[0].State != cleaner.AgentStateRevalidationBlocked ||
				unit.Obligations[0].Classification != classification {
				t.Fatalf("drift receipt = %+v; want blocked classified obligation", unit)
			}
			assertPathExists(t, outer)
		})
	}

	t.Run("new orphan with missing revalidator", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home)
		outer := filepath.Join(home, ".cache", "provenance-missing")
		entry := filepath.Join(outer, "agent", "new-orphan")
		if err := os.MkdirAll(entry, 0o755); err != nil {
			t.Fatal(err)
		}
		orphan := overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)
		runtime := cleanupOverlapSafetyRuntime{
			OverlapRuntime: cleaner.OverlapRuntime{
				Initial: cleaner.OverlapSafetyEvidence{Complete: true},
				Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
					return cleaner.OverlapSafetyEvidence{
						Items:    []types.DebrisInfo{orphan},
						Complete: true,
					}, nil
				},
			},
		}
		selection, err := applyCleanupOverlapSafety(
			context.Background(),
			runtime,
			[]types.DebrisInfo{overlapCmdTarget(outer, 350)},
		)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if err == nil {
			t.Fatal("missing refreshed revalidator error = nil")
		}
		unit := singleExecutionUnit(t, receipt)
		canonicalEntry, _ := cleaner.TargetPathKey(entry)
		if unit.PhysicalRemoved || unit.FreedBytes != 0 ||
			len(unit.Obligations) != 1 ||
			unit.Obligations[0].EntryPath != canonicalEntry ||
			unit.Obligations[0].State != cleaner.AgentStateRevalidationBlocked ||
			!strings.Contains(unit.Obligations[0].Reason, "revalidator missing") {
			t.Fatalf("missing-revalidator receipt = %+v", unit)
		}
		assertPathExists(t, outer)
	})
}

func TestOverlapProvenanceProtectedAncestorAndDescendantKeepOwnerAccounting(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name         string
		targetPath   string
		entryPath    string
		wantRelation cleanupOverlapRelation
		wantReason   cleaner.OverlapSafetyReason
	}{
		{
			name:         "protected parent generic child",
			targetPath:   filepath.Join(root, "protected-parent", "node_modules"),
			entryPath:    filepath.Join(root, "protected-parent"),
			wantRelation: cleanupOverlapAncestor,
			wantReason:   cleaner.OverlapSafetyProtectedAncestor,
		},
		{
			name:         "generic parent protected child",
			targetPath:   filepath.Join(root, "generic-parent"),
			entryPath:    filepath.Join(root, "generic-parent", "agent"),
			wantRelation: cleanupOverlapDescendant,
			wantReason:   cleaner.OverlapSafetyProtectedDescendant,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, path := range []string{tt.targetPath, tt.entryPath} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			target := overlapCmdTarget(tt.targetPath, 600)
			entry := overlapCmdAgentStateItem(tt.entryPath, types.EntryClassLive)
			runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{entry}, nil)
			selection, err := applyCleanupOverlapSafetyWithRows(
				context.Background(),
				runtime,
				[]types.DebrisInfo{target},
				[]cleanupOverlapLogicalInput{
					{Item: entry, PolicyReason: "live agent-state protected"},
					{Item: target, PolicyReason: "generic target eligible"},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(selection.Targets) != 0 || len(selection.Components) != 1 {
				t.Fatalf("selection = %+v; protected component must not execute", selection)
			}
			component := selection.Components[0]
			if component.Owner.Path != tt.targetPath || component.Owner.Size != 600 ||
				component.Refusal == nil || component.Refusal.Reason != tt.wantReason {
				t.Fatalf("component = %+v; generic raw target remains physical owner", component)
			}
			var found bool
			var physicalBytes int64
			for _, row := range component.LogicalRows {
				physicalBytes += row.PhysicalBytes
				if row.Item.Path == tt.entryPath {
					found = true
					if row.Relation != tt.wantRelation ||
						row.L1Reason != string(tt.wantReason) ||
						row.PolicyReason != "live agent-state protected" {
						t.Fatalf("protected logical row = %+v", row)
					}
				}
			}
			if !found || physicalBytes != 600 {
				t.Fatalf("found/bytes = %t/%d; protected evidence must add zero bytes", found, physicalBytes)
			}
		})
	}
}

func TestOverlapProvenancePlanningRefusalKeepsLineageWithoutReceipt(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outer := filepath.Join(home, ".cache", "missing-revalidator")
	entry := filepath.Join(outer, "agent", "orphan")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	target := overlapCmdTarget(outer, 250)
	orphan := overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{orphan}, nil)
	selection, err := applyCleanupOverlapSafetyWithRows(
		context.Background(),
		runtime,
		[]types.DebrisInfo{target},
		[]cleanupOverlapLogicalInput{
			{Item: target, PolicyReason: "generic target eligible"},
			{Item: orphan, PolicyReason: "recorded working directory is absent"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Targets) != 0 || len(selection.Components) != 1 ||
		selection.Components[0].Refusal == nil ||
		selection.Components[0].Refusal.Reason != cleaner.OverlapSafetyNestedRevalidation {
		t.Fatalf("selection = %+v; missing revalidator must refuse during planning", selection)
	}
	var orphanRow *cleanupOverlapLogicalRow
	for i := range selection.Components[0].LogicalRows {
		if selection.Components[0].LogicalRows[i].Item.Path == entry {
			orphanRow = &selection.Components[0].LogicalRows[i]
			break
		}
	}
	if orphanRow == nil || !orphanRow.RevalidationRequired ||
		orphanRow.PolicyReason != "recorded working directory is absent" ||
		orphanRow.L1Reason != string(cleaner.OverlapSafetyNestedRevalidation) {
		t.Fatalf("orphan row = %+v; planning refusal lost obligation lineage", orphanRow)
	}
	prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
	if len(prepared) != 0 {
		t.Fatalf("planning refusal prepared mutation receipt state: %+v", prepared)
	}
	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if err != nil || len(receipt.Units) != 0 || receipt.FreedBytes != 0 {
		t.Fatalf("receipt = %+v, error=%v; no execution receipt should be fabricated", receipt, err)
	}
	output := captureOutput(func() {
		printOverlapSafetyRefusals(selection)
	})
	for _, want := range []string{
		"nested agent-state revalidation refused",
		"recorded working directory is absent",
		"generic target eligible",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("planning preview missing %q:\n%s", want, output)
		}
	}
}

func TestOverlapProvenanceRetainsAmbiguousLogicalEvidence(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outer := filepath.Join(home, ".cache", "ambiguous-lineage")
	if err := os.MkdirAll(outer, 0o755); err != nil {
		t.Fatal(err)
	}
	ambiguous := makeOverlapCmdSymlinkCycleError(
		t,
		filepath.Join(home, ".agent-state-aliases"),
	)
	entry := overlapCmdAgentStateItem(ambiguous, types.EntryClassOrphaned)
	target := overlapCmdTarget(outer, 90)
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{entry}, nil)
	selection, err := applyCleanupOverlapSafetyWithRows(
		context.Background(),
		runtime,
		[]types.DebrisInfo{target},
		[]cleanupOverlapLogicalInput{
			{Item: entry, PolicyReason: "orphan policy evidence"},
			{Item: target, PolicyReason: "outer policy evidence"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	component := selection.Components[0]
	if component.Refusal == nil ||
		component.Refusal.Reason != cleaner.OverlapSafetyAmbiguousIdentity ||
		len(component.LogicalRows) != 2 {
		t.Fatalf("component = %+v; ambiguous row must stay attached", component)
	}
	var ambiguousRow *cleanupOverlapLogicalRow
	var physicalBytes int64
	for i := range component.LogicalRows {
		physicalBytes += component.LogicalRows[i].PhysicalBytes
		if component.LogicalRows[i].Item.Path == ambiguous {
			ambiguousRow = &component.LogicalRows[i]
		}
	}
	if ambiguousRow == nil ||
		ambiguousRow.Relation != cleanupOverlapAmbiguous ||
		ambiguousRow.L1Reason != string(cleaner.OverlapSafetyAmbiguousIdentity) ||
		ambiguousRow.RevalidationRequired ||
		physicalBytes != 90 {
		t.Fatalf("ambiguous row/bytes = %+v/%d", ambiguousRow, physicalBytes)
	}
}

func TestPhysicalCleanAuditUsesOwnerBytesAndLogicalEvidence(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outer := filepath.Join(home, ".cache", "audit-owner")
	entry := filepath.Join(outer, "agent", "orphan")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := overlapCmdTarget(outer, 1000)
	owner.ModTime = time.Now().Add(-48 * time.Hour)
	orphan := overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)
	orphan.Size = 300
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
			return types.EntryClassOrphaned, nil
		},
	})
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{orphan}, lookup)
	opts := types.PruneOptions{Age: 24 * time.Hour}
	selection, err := applyCleanupOverlapSafetyWithRows(
		context.Background(),
		runtime,
		[]types.DebrisInfo{owner},
		cleanupOverlapLogicalInputsForAudit([]types.DebrisInfo{orphan, owner}, opts, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	audit := buildPhysicalCleanAudit(
		[]types.DebrisInfo{orphan, owner},
		selection.Components,
		selection.Targets,
		opts,
		2,
		scanSource{Kind: scanSourceLive},
		selection.Protections,
	)
	if audit.TotalFoundCount != 1 || audit.TotalFoundSize != 1000 ||
		audit.TotalEligibleCount != 1 || audit.TotalEligibleSize != 1000 ||
		audit.TotalBlockedCount != 0 || audit.TotalBlockedSize != 0 ||
		audit.TotalEvidenceCount != 2 {
		t.Fatalf("audit = %+v; nested evidence must not add physical bytes", audit)
	}
	agentState := findAuditCategory(t, audit, types.CategoryAgentState)
	if agentState.FoundCount != 0 || agentState.FoundSize != 0 ||
		agentState.EvidenceCount != 1 ||
		agentState.MainReason != string(cleanReasonNestedRevalidationRequired) {
		t.Fatalf("agent-state row = %+v; want zero-byte revalidation evidence", agentState)
	}
	buildCache := findAuditCategory(t, audit, types.CategoryBuildCache)
	if buildCache.FoundCount != 1 || buildCache.FoundSize != 1000 ||
		buildCache.EligibleCount != 1 || buildCache.EligibleSize != 1000 {
		t.Fatalf("owner category row = %+v", buildCache)
	}
}

func assertCleanupComponentLineageEqual(
	t *testing.T,
	left []cleanupOverlapComponent,
	right []cleanupOverlapComponent,
) {
	t.Helper()
	if len(left) != len(right) {
		t.Fatalf("component lengths differ: %d/%d", len(left), len(right))
	}
	for i := range left {
		if left[i].CanonicalPath != right[i].CanonicalPath ||
			cleaner.TargetStableKey(left[i].Owner) != cleaner.TargetStableKey(right[i].Owner) ||
			len(left[i].LogicalRows) != len(right[i].LogicalRows) ||
			len(left[i].Obligations) != len(right[i].Obligations) {
			t.Fatalf("component %d differs:\nleft=%+v\nright=%+v", i, left[i], right[i])
		}
		for j := range left[i].LogicalRows {
			if left[i].LogicalRows[j].Key != right[i].LogicalRows[j].Key ||
				left[i].LogicalRows[j].PolicyReason != right[i].LogicalRows[j].PolicyReason ||
				left[i].LogicalRows[j].L1Reason != right[i].LogicalRows[j].L1Reason {
				t.Fatalf("logical row %d/%d differs:\nleft=%+v\nright=%+v",
					i, j, left[i].LogicalRows[j], right[i].LogicalRows[j])
			}
		}
		for j := range left[i].Obligations {
			if left[i].Obligations[j].Tool != right[i].Obligations[j].Tool ||
				left[i].Obligations[j].EntryPath != right[i].Obligations[j].EntryPath ||
				left[i].Obligations[j].ProviderID != right[i].Obligations[j].ProviderID {
				t.Fatalf("obligation %d/%d differs", i, j)
			}
		}
	}
}
