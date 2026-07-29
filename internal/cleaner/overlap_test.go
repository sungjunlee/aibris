package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

type overlapTestRevalidator func(context.Context, string) (types.EntryClass, error)

func (fn overlapTestRevalidator) RevalidateAgentState(ctx context.Context, path string) (types.EntryClass, error) {
	return fn(ctx, path)
}

func TestBuildOverlapSafetyPlanRequiresExplicitCompleteEvidence(t *testing.T) {
	target := makeOverlapTestDir(t, filepath.Join(t.TempDir(), "target"))

	_, err := BuildOverlapSafetyPlan(context.Background(), OverlapSafetyEvidence{}, []types.DebrisInfo{{
		Path: target,
	}}, nil)
	if !errors.Is(err, ErrIncompleteOverlapSafetyEvidence) {
		t.Fatalf("BuildOverlapSafetyPlan() error = %v; want incomplete-evidence refusal", err)
	}

	_, err = BuildOverlapSafetyPlan(context.Background(), OverlapSafetyEvidence{
		Complete: true,
		ProviderErrors: []types.ScanProviderError{{
			Tool:    types.ToolCursor,
			Message: "injected scan failure",
		}},
	}, []types.DebrisInfo{{Path: target}}, nil)
	if !errors.Is(err, ErrIncompleteOverlapSafetyEvidence) {
		t.Fatalf("BuildOverlapSafetyPlan() partial error = %v; want incomplete-evidence refusal", err)
	}
}

func TestBuildOverlapSafetyPlanHardLocksBothContainmentDirectionsAndExactPath(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name       string
		targetPath string
		entryPath  string
		wantReason OverlapSafetyReason
	}{
		{
			name:       "protected parent generic child",
			targetPath: filepath.Join(root, "protected-parent", "deep", "generic"),
			entryPath:  filepath.Join(root, "protected-parent"),
			wantReason: OverlapSafetyProtectedAncestor,
		},
		{
			name:       "generic parent protected child",
			targetPath: filepath.Join(root, "generic-parent"),
			entryPath:  filepath.Join(root, "generic-parent", "deep", "protected"),
			wantReason: OverlapSafetyProtectedDescendant,
		},
		{
			name:       "exact path duplicate",
			targetPath: filepath.Join(root, "exact"),
			entryPath:  filepath.Join(root, "exact"),
			wantReason: OverlapSafetyProtectedExact,
		},
	}
	for _, classification := range []types.EntryClass{
		types.EntryClassLive,
		types.EntryClassUndetermined,
	} {
		for _, tt := range tests {
			t.Run(string(classification)+"/"+tt.name, func(t *testing.T) {
				makeOverlapTestDir(t, tt.targetPath)
				makeOverlapTestDir(t, tt.entryPath)
				entry := overlapAgentStateItem(tt.entryPath, classification)
				plan := buildOverlapTestPlan(t, []types.DebrisInfo{entry}, []types.DebrisInfo{{
					Path: tt.targetPath,
				}}, nil)
				component := singleOverlapComponent(t, plan)
				if component.Refusal == nil || component.Refusal.Reason != tt.wantReason {
					t.Fatalf("refusal = %+v; want %s", component.Refusal, tt.wantReason)
				}
				if len(plan.AllowedTargets()) != 0 {
					t.Fatalf("AllowedTargets() = %+v; want none", plan.AllowedTargets())
				}
			})
		}
	}
}

func TestBuildOverlapSafetyPlanCanonicalizesSymlinkAliasesAndFailsClosedOnAmbiguity(t *testing.T) {
	root := t.TempDir()
	realEntry := makeOverlapTestDir(t, filepath.Join(root, "real", "entry"))
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realEntry, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	plan := buildOverlapTestPlan(t,
		[]types.DebrisInfo{overlapAgentStateItem(realEntry, types.EntryClassLive)},
		[]types.DebrisInfo{{Path: alias}},
		nil,
	)
	if refusal := singleOverlapComponent(t, plan).Refusal; refusal == nil ||
		refusal.Reason != OverlapSafetyProtectedExact {
		t.Fatalf("symlink alias refusal = %+v; want exact protected overlap", refusal)
	}

	missingBelowAlias := filepath.Join(alias, "missing")
	plan = buildOverlapTestPlan(t,
		[]types.DebrisInfo{overlapAgentStateItem(missingBelowAlias, types.EntryClassOrphaned)},
		[]types.DebrisInfo{{Path: realEntry}},
		nil,
	)
	if refusal := singleOverlapComponent(t, plan).Refusal; refusal == nil ||
		refusal.Reason != OverlapSafetyAmbiguousIdentity {
		t.Fatalf("intermediate symlink ambiguity refusal = %+v; want fail-closed ambiguity", refusal)
	}

	ambiguousTarget := makeOverlapTestDir(t, filepath.Join(root, "ambiguous-target"))
	brokenEntry := filepath.Join(ambiguousTarget, "broken-entry")
	if err := os.Symlink(filepath.Join(root, "missing"), brokenEntry); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	plan = buildOverlapTestPlan(t,
		[]types.DebrisInfo{overlapAgentStateItem(brokenEntry, types.EntryClassOrphaned)},
		[]types.DebrisInfo{{Path: ambiguousTarget}},
		nil,
	)
	if refusal := singleOverlapComponent(t, plan).Refusal; refusal == nil ||
		refusal.Reason != OverlapSafetyAmbiguousIdentity {
		t.Fatalf("ambiguous identity refusal = %+v; want fail-closed ambiguity", refusal)
	}

	unrelated := makeOverlapTestDir(t, filepath.Join(root, "unrelated"))
	plan = buildOverlapTestPlan(t,
		[]types.DebrisInfo{overlapAgentStateItem(brokenEntry, types.EntryClassOrphaned)},
		[]types.DebrisInfo{{Path: unrelated}},
		nil,
	)
	if refusal := singleOverlapComponent(t, plan).Refusal; refusal != nil {
		t.Fatalf("unrelated target was blocked by another component's ambiguity: %+v", refusal)
	}
}

func TestBuildOverlapSafetyPlanFailsClosedOnCanonicalizationError(t *testing.T) {
	root := t.TempDir()
	target := makeOverlapTestDir(t, filepath.Join(root, "target"))
	entry := makeOverlapTestSymlinkDepthError(t, filepath.Join(root, "aliases"), target)

	if _, err := canonicalExistingPathIdentity(entry); err == nil {
		t.Fatal("deep symlink agent-state identity unexpectedly resolved")
	}
	if _, err := resolvePathWithUnresolvedSuffix(entry, 0); err == nil {
		t.Fatal("deep symlink agent-state path unexpectedly resolved")
	} else if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resolution error = %v; want deterministic non-ENOENT failure", err)
	}

	plan := buildOverlapTestPlan(t,
		[]types.DebrisInfo{overlapAgentStateItem(entry, types.EntryClassOrphaned)},
		[]types.DebrisInfo{{Path: target}},
		nil,
	)
	component := singleOverlapComponent(t, plan)
	if component.Refusal == nil ||
		component.Refusal.Reason != OverlapSafetyAmbiguousIdentity ||
		!strings.Contains(component.Refusal.Detail, "too many symlinks") {
		t.Fatalf("canonicalization error refusal = %+v; want fail-closed detail", component.Refusal)
	}
	if len(component.Matches) != 1 ||
		component.Matches[0].Relation != OverlapRelationAmbiguous ||
		len(plan.AllowedTargets()) != 0 {
		t.Fatalf("component = %+v; want one ambiguous match and no allowed target", component)
	}
}

func TestBuildOverlapSafetyPlanPreservesStableDeduplicatedObligations(t *testing.T) {
	root := t.TempDir()
	target := makeOverlapTestDir(t, filepath.Join(root, "outer"))
	first := makeOverlapTestDir(t, filepath.Join(target, "agent", "first"))
	second := makeOverlapTestDir(t, filepath.Join(target, "agent", "second"))
	lookup := overlapTestLookup(map[types.Tool]overlapTestRevalidator{
		types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
			return types.EntryClassOrphaned, nil
		},
		types.ToolCursor: func(context.Context, string) (types.EntryClass, error) {
			return types.EntryClassOrphaned, nil
		},
	})
	inventory := []types.DebrisInfo{
		overlapAgentStateItem(second, types.EntryClassOrphaned),
		overlapAgentStateItem(first, types.EntryClassOrphaned),
		overlapAgentStateItem(first, types.EntryClassOrphaned),
	}
	inventory[0].Tool = types.ToolCursor

	forward := buildOverlapTestPlan(t, inventory, []types.DebrisInfo{{Path: target}}, lookup)
	reversed := buildOverlapTestPlan(t,
		[]types.DebrisInfo{inventory[2], inventory[1], inventory[0]},
		[]types.DebrisInfo{{Path: target}},
		lookup,
	)
	for _, plan := range []OverlapSafetyPlan{forward, reversed} {
		component := singleOverlapComponent(t, plan)
		if component.Refusal != nil {
			t.Fatalf("unexpected refusal: %v", component.Refusal)
		}
		if len(component.Obligations) != 2 {
			t.Fatalf("obligations = %+v; want two deduplicated entries", component.Obligations)
		}
		if component.Obligations[0].EntryPath >= component.Obligations[1].EntryPath {
			t.Fatalf("obligations are not stable: %+v", component.Obligations)
		}
		for _, obligation := range component.Obligations {
			if obligation.ProviderID == "" {
				t.Fatalf("obligation lacks exact provider identity: %+v", obligation)
			}
		}
	}
}

func TestBuildOverlapSafetyPlanRefusesCommandOverlapAndMissingRevalidator(t *testing.T) {
	root := t.TempDir()
	target := makeOverlapTestDir(t, filepath.Join(root, "outer"))
	entry := makeOverlapTestDir(t, filepath.Join(target, "orphan"))
	inventory := []types.DebrisInfo{overlapAgentStateItem(entry, types.EntryClassOrphaned)}

	commandPlan := buildOverlapTestPlan(t, inventory, []types.DebrisInfo{{
		Path:           target,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"missing-command"},
	}}, nil)
	if refusal := singleOverlapComponent(t, commandPlan).Refusal; refusal == nil ||
		refusal.Reason != OverlapSafetyCommandOverlap {
		t.Fatalf("command refusal = %+v; want command overlap", refusal)
	}

	removePlan := buildOverlapTestPlan(t, inventory, []types.DebrisInfo{{
		Path: target,
	}}, nil)
	if refusal := singleOverlapComponent(t, removePlan).Refusal; refusal == nil ||
		refusal.Reason != OverlapSafetyNestedRevalidation ||
		!strings.Contains(refusal.Detail, "revalidator missing") {
		t.Fatalf("missing revalidator refusal = %+v", refusal)
	}
}

func TestOverlapSafetyValidationRefreshesAndRevalidatesBeforeMutation(t *testing.T) {
	root := t.TempDir()
	target := makeOverlapTestDir(t, filepath.Join(root, "outer"))
	first := makeOverlapTestDir(t, filepath.Join(target, "first"))
	second := makeOverlapTestDir(t, filepath.Join(target, "second"))
	inventory := []types.DebrisInfo{
		overlapAgentStateItem(first, types.EntryClassOrphaned),
		overlapAgentStateItem(second, types.EntryClassOrphaned),
	}
	var calls []string
	lateFailure := errors.New("late revalidation failure")
	lookup := overlapTestLookup(map[types.Tool]overlapTestRevalidator{
		types.ToolClaude: func(_ context.Context, path string) (types.EntryClass, error) {
			calls = append(calls, path)
			if filepath.Base(path) == filepath.Base(second) {
				return "", lateFailure
			}
			return types.EntryClassOrphaned, nil
		},
	})
	component := singleOverlapComponent(t, buildOverlapTestPlan(t, inventory, []types.DebrisInfo{{Path: target}}, lookup))

	err := component.ValidateBeforeMutation(context.Background(), OverlapSafetyEvidence{
		Items:    inventory,
		Complete: true,
	}, lookup)
	if !errors.Is(err, lateFailure) {
		t.Fatalf("ValidateBeforeMutation() error = %v, calls = %v; want late failure", err, calls)
	}
	if len(calls) != 2 {
		t.Fatalf("revalidation calls = %v; want every obligation checked", calls)
	}

	calls = nil
	refreshed := append([]types.DebrisInfo(nil), inventory...)
	refreshed[0].Classification = types.EntryClassLive
	err = component.ValidateBeforeMutation(context.Background(), OverlapSafetyEvidence{
		Items:    refreshed,
		Complete: true,
	}, lookup)
	if !errors.Is(err, ErrOverlapSafetyRefusal) ||
		!strings.Contains(err.Error(), string(OverlapSafetyProtectedDescendant)) {
		t.Fatalf("classification drift error = %v; want protected refusal", err)
	}
	if len(calls) != 0 {
		t.Fatalf("revalidator ran after refreshed hard lock: %v", calls)
	}
}

func TestOverlapSafetyValidationRejectsCancellationAndSymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	first := makeOverlapTestDir(t, filepath.Join(root, "first"))
	second := makeOverlapTestDir(t, filepath.Join(root, "second"))
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	component := singleOverlapComponent(t, buildOverlapTestPlan(t, nil, []types.DebrisInfo{{Path: alias}}, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := component.ValidateBeforeMutation(ctx, OverlapSafetyEvidence{Complete: true}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v; want context.Canceled", err)
	}

	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	err := component.ValidateBeforeMutation(context.Background(), OverlapSafetyEvidence{Complete: true}, nil)
	if !errors.Is(err, ErrOverlapSafetyRefusal) ||
		!strings.Contains(err.Error(), string(OverlapSafetyAmbiguousIdentity)) {
		t.Fatalf("symlink retarget error = %v; want identity refusal", err)
	}
}

func buildOverlapTestPlan(
	t *testing.T,
	inventory []types.DebrisInfo,
	targets []types.DebrisInfo,
	lookup AgentStateRevalidatorLookup,
) OverlapSafetyPlan {
	t.Helper()
	plan, err := BuildOverlapSafetyPlan(context.Background(), OverlapSafetyEvidence{
		Items:    inventory,
		Complete: true,
	}, targets, lookup)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func singleOverlapComponent(t *testing.T, plan OverlapSafetyPlan) OverlapSafetyComponent {
	t.Helper()
	if len(plan.Components) != 1 {
		t.Fatalf("components = %d; want 1", len(plan.Components))
	}
	return plan.Components[0]
}

func overlapAgentStateItem(path string, classification types.EntryClass) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             filepath.Base(path),
		Path:           path,
		Classification: classification,
	}
}

func overlapTestLookup(
	revalidators map[types.Tool]overlapTestRevalidator,
) AgentStateRevalidatorLookup {
	return func(tool types.Tool) (adapter.AgentStateRevalidatorRegistration, error) {
		revalidator, ok := revalidators[tool]
		if !ok {
			return adapter.AgentStateRevalidatorRegistration{},
				fmt.Errorf("%w for tool %q", adapter.ErrAgentStateRevalidatorMissing, tool)
		}
		return adapter.AgentStateRevalidatorRegistration{
			Tool:        tool,
			ProviderID:  "test-provider:" + string(tool),
			Revalidator: revalidator,
		}, nil
	}
}

func makeOverlapTestDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeOverlapTestSymlinkDepthError(t *testing.T, aliasesRoot, target string) string {
	t.Helper()
	if err := os.MkdirAll(aliasesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	next := target
	for i := 300; i >= 0; i-- {
		alias := filepath.Join(aliasesRoot, fmt.Sprintf("alias-%03d", i))
		if err := os.Symlink(next, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		next = alias
	}
	return next
}
