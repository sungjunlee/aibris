package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanupPlanEvidenceCarriesPartialScanAndCacheFreshness(t *testing.T) {
	now := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)

	live := cleanupPlanEvidence(&types.ScanResult{}, scanSource{Kind: scanSourceLive}, now)
	if live.MaxAge != 0 {
		t.Fatalf("live evidence max age = %v; want no expiry", live.MaxAge)
	}
	if len(live.ProviderErrors) != 0 {
		t.Fatalf("complete live evidence carried provider errors: %+v", live.ProviderErrors)
	}

	cached := cleanupPlanEvidence(&types.ScanResult{}, scanSource{Kind: scanSourceCached}, now)
	if cached.MaxAge != lastScanCacheMaxAge {
		t.Fatalf("cached evidence max age = %v; want %v", cached.MaxAge, lastScanCacheMaxAge)
	}
	if err := (UnifiedCleanupPlan{Evidence: cached}).ValidateForExecution(context.Background(), now); !errors.Is(err, errStaleCleanupPlanEvidence) {
		t.Fatalf("cached evidence without observed time error = %v; want fail-closed stale evidence", err)
	}

	partial := cleanupPlanEvidence(&types.ScanResult{
		ProviderErrors: []types.ScanProviderError{{Tool: "codex", Message: "boom"}},
	}, scanSource{Kind: scanSourceLive}, now)
	if len(partial.ProviderErrors) != 1 {
		t.Fatalf("partial evidence provider errors = %+v; want the scan failure carried through", partial.ProviderErrors)
	}
}

func TestCleanupPlanEvidencePreservesCachedSourceAge(t *testing.T) {
	cacheReadAt := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cacheAge := 4*time.Minute + 30*time.Second
	cacheCreatedAt := cacheReadAt.Add(-cacheAge)
	planCreatedAt := cacheReadAt.Add(90 * time.Second)
	evidence := cleanupPlanEvidence(
		&types.ScanResult{},
		scanSource{Kind: scanSourceCached, Age: cacheAge, ObservedAt: cacheCreatedAt},
		planCreatedAt,
	)

	if !evidence.ObservedAt.Equal(cacheCreatedAt) {
		t.Fatalf("cached evidence observed at = %v; want cache creation time %v", evidence.ObservedAt, cacheCreatedAt)
	}
	plan := UnifiedCleanupPlan{Evidence: evidence}
	if err := plan.ValidateForExecution(context.Background(), planCreatedAt); !errors.Is(err, errStaleCleanupPlanEvidence) {
		t.Fatalf("cached evidence validation error = %v; want stale after prompt exceeds original freshness window", err)
	}
}

func TestInteractiveCleanWithValidationRunsAfterApprovalAndStopsMutation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	paths := []string{
		filepath.Join(home, "one", "node_modules"),
		filepath.Join(home, "two", "node_modules"),
	}
	items := make([]types.DebrisInfo, 0, len(paths))
	for _, path := range paths {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		items = append(items, types.DebrisInfo{
			Path:     path,
			Category: types.CategoryNodeModules,
		})
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, items)
	if err != nil {
		t.Fatal(err)
	}
	targets := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
	defer withStdin(t, "y\ny\n")()

	validationErr := errors.New("expired after prompt")
	validationCalls := 0
	receipt, err := interactiveCleanWithValidation(context.Background(), targets, func(context.Context) error {
		validationCalls++
		return validationErr
	})
	if !errors.Is(err, validationErr) {
		t.Fatalf("interactive validation error = %v; want %v", err, validationErr)
	}
	if validationCalls != 1 {
		t.Fatalf("validation calls = %d; want one call after first approval", validationCalls)
	}
	if len(receipt.Units) != len(targets) {
		t.Fatalf("failed receipt units = %d; want current and remaining %d", len(receipt.Units), len(targets))
	}
	for _, path := range paths {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("validation failure mutated %q: %v", path, statErr)
		}
	}
}

func TestExecuteUnifiedPreparedCleanTargetsRejectsStalePlan(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, "workspace", "node_modules")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatal(err)
	}
	plan := UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
		ObservedAt: time.Now().Add(-time.Hour),
		MaxAge:     time.Minute,
	}}
	item := types.DebrisInfo{
		Path:     targetPath,
		Category: types.CategoryNodeModules,
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	targets := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)

	receipt, err := executeUnifiedPreparedCleanTargets(context.Background(), plan, targets)
	if !errors.Is(err, errStaleCleanupPlanEvidence) {
		t.Fatalf("execution-boundary error = %v; want stale evidence", err)
	}
	if len(receipt.Units) != 1 {
		t.Fatalf("failed receipt units = %d; want 1", len(receipt.Units))
	}
	if _, statErr := os.Stat(targetPath); statErr != nil {
		t.Fatalf("stale plan mutated target: %v", statErr)
	}
}

func TestGuidedCleanupPlanCandidatesReflectAcceptedSelection(t *testing.T) {
	locked := guidedCleanState{ScanSource: scanSource{Kind: scanSourceLive}}
	locked.Rows = []guidedCleanRow{{
		Key:      "locked-unit",
		Policy:   guidedCleanPolicyLocked,
		Selected: false,
		Row:      guidedCodexWorktreeRow{Item: types.DebrisInfo{Path: "/home/.codex/worktrees/locked"}, Reason: "dirty worktree"},
	}}

	selected := guidedCleanState{}
	selected.Rows = []guidedCleanRow{{
		Key:      "selected-unit",
		Policy:   guidedCleanPolicyRecommended,
		Selected: true,
		Row:      guidedCodexWorktreeRow{Item: types.DebrisInfo{Path: "/home/.codex/worktrees/pick"}, Reason: "cleanup recommended"},
	}}

	reviewable := guidedCleanState{}
	reviewable.Rows = []guidedCleanRow{{
		Key:      "reviewable-unit",
		Policy:   guidedCleanPolicyReviewable,
		Selected: false,
		Row:      guidedCodexWorktreeRow{Item: types.DebrisInfo{Path: "/home/.codex/worktrees/hold"}, Reason: "retained per repository"},
	}}

	candidates := append(append(
		guidedCleanupPlanCandidates(locked),
		guidedCleanupPlanCandidates(selected)...),
		guidedCleanupPlanCandidates(reviewable)...,
	)
	byRow := make(map[string]CleanupPlanSelection, len(candidates))
	for _, candidate := range candidates {
		byRow[candidate.RowKey] = candidate.Selection
	}
	if byRow["guided:locked-unit"] != CleanupPlanLocked {
		t.Errorf("locked guided row = %q; want locked", byRow["guided:locked-unit"])
	}
	if byRow["guided:selected-unit"] != CleanupPlanSelected {
		t.Errorf("selected guided row = %q; want selected", byRow["guided:selected-unit"])
	}
	if byRow["guided:reviewable-unit"] != CleanupPlanUnselected {
		t.Errorf("reviewable guided row = %q; want unselected", byRow["guided:reviewable-unit"])
	}
}

func TestUnifiedCleanupPlanForCleanMergesGuidedAndClassicCandidates(t *testing.T) {
	guided := guidedCleanState{}
	guided.Rows = []guidedCleanRow{{
		Key:      "guided-unit",
		Policy:   guidedCleanPolicyRecommended,
		Selected: true,
		Row:      guidedCodexWorktreeRow{Item: types.DebrisInfo{Path: "/home/.codex/worktrees/g"}, Reason: "cleanup recommended"},
	}}
	classic := []types.DebrisInfo{{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		Path:     "/home/workspace/app/node_modules",
		Size:     1024,
	}}

	plan, err := unifiedCleanupPlanForClean(
		context.Background(),
		&guided,
		classic,
		CleanupPlanEvidence{ObservedAt: time.Now()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 2 {
		t.Fatalf("plan rows = %d; want guided + classic rows", len(plan.Rows))
	}
	selected := plan.SelectedPhysicalTargets()
	if len(selected) != 2 {
		t.Fatalf("selected = %d; want both guided and classic selected", len(selected))
	}
}

func TestValidateAndSelectForExecutionGatesPartialAndStaleEvidence(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	item := types.DebrisInfo{Path: "/home/target", Category: types.CategoryBuildCache}
	plan, err := BuildUnifiedCleanupPlan(context.Background(), []CleanupPlanCandidate{{
		RowKey:    "c",
		Item:      item,
		Selection: CleanupPlanSelected,
	}}, CleanupPlanEvidence{ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	if selected, err := validateAndSelectForExecution(context.Background(), plan, now); err != nil || len(selected) != 1 {
		t.Fatalf("valid plan = %+v, err %v; want one selected target", selected, err)
	}

	partial := plan
	partial.Evidence.ProviderErrors = []types.ScanProviderError{{Tool: "codex", Message: "boom"}}
	if _, err := validateAndSelectForExecution(context.Background(), partial, now); err == nil {
		t.Fatal("partial evidence must block execution")
	} else if !strings.Contains(err.Error(), "provider(s) failed") {
		t.Fatalf("partial evidence error = %v; want provider failure", err)
	}

	stale := plan
	stale.Evidence.MaxAge = 5 * time.Minute
	if _, err := validateAndSelectForExecution(context.Background(), stale, now.Add(time.Hour)); err == nil {
		t.Fatal("stale evidence must block execution")
	}

	locked := plan
	locked.Rows[0].Selection = CleanupPlanLocked
	locked.Components[0].Selection = CleanupPlanLocked
	if selected, err := validateAndSelectForExecution(context.Background(), locked, now); err != nil || len(selected) != 0 {
		t.Fatalf("locked plan selected %d, err %v; want no selectable targets", len(selected), err)
	}
}

func TestUnifiedCleanupPlanEvidenceRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := unifiedCleanupPlanForClean(ctx, nil, nil, CleanupPlanEvidence{})
	if err == nil {
		t.Fatal("cancelled context must abort plan construction")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("plan construction error = %v; want context.Canceled", err)
	}
}
