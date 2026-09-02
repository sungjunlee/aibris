package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func preparedExecutorTarget(
	t testing.TB,
	item types.DebrisInfo,
	selected WorktreeCleanupUnit,
) preparedCleanTarget {
	t.Helper()
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	safety, err := mutationSafetyForTarget(selection, runtime, item)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureCleanupTargetSnapshot(item, types.PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return preparedCleanTarget{
		Item:           item,
		ActiveUnit:     &selected,
		MutationSafety: safety,
		TargetSnapshot: snapshot,
	}
}

func TestCaptureCleanupTargetSnapshotReportsAgeChangeSinceScan(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "recent-target")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:        types.ToolBuildCache,
		Category:    types.CategoryBuildCache,
		Path:        targetPath,
		CleanupKind: types.CleanupRemovePath,
	}

	_, err := captureCleanupTargetSnapshot(item, types.PruneOptions{Age: 24 * time.Hour})
	if err == nil {
		t.Fatal("captureCleanupTargetSnapshot() error = nil; want minimum-age refusal")
	}
	if !strings.Contains(err.Error(), "changed since scan") ||
		strings.Contains(err.Error(), "changed since cleanup selection") {
		t.Fatalf("error=%v; want changed-since-scan age reason", err)
	}
}

func TestExecuteActiveWorktreeRemovesMultiMemberUnitWithDefaultAge(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 900)
	selected := buildExecutorUnit(t, item)

	receipt, err := executePreparedCleanTargets(
		context.Background(),
		[]preparedCleanTarget{preparedExecutorTarget(t, item, selected)},
		defaultActiveWorktreeExecutionOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionRemoved || !unit.PhysicalRemoved || unit.FreedBytes <= 0 || receipt.FreedBytes != unit.FreedBytes {
		t.Fatalf("unit = %+v, total freed=%d; want multi-member removal with default age", unit, receipt.FreedBytes)
	}
	if len(unit.Members) != 2 {
		t.Fatalf("member receipts = %+v; want two members", unit.Members)
	}
	for _, member := range unit.Members {
		if !member.Removed || member.Error != "" {
			t.Errorf("member receipt = %+v; want removed", member)
		}
	}
	for _, worktree := range []string{first, second} {
		if !pathDoesNotExist(worktree) {
			t.Errorf("worktree %q still exists", worktree)
		}
		assertRepositoryDoesNotListWorktree(t, repository, worktree)
	}
}

func TestExecutePreparedCommandCancellationAfterStartRemainsFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command fixture is Unix-specific")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "command-cancelled")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("cancelled-command-payload")
	if err := os.WriteFile(filepath.Join(targetPath, "payload"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "command-started")
	command := filepath.Join(t.TempDir(), "cancel-after-start")
	writeJSONReceiptExecutable(t, command, "#!/bin/sh\ntouch \"$1\"\nsleep 5\n")
	target := types.DebrisInfo{
		ID:             "command-cancelled",
		Tool:           types.ToolBuildCache,
		Category:       types.CategoryBuildCache,
		Path:           targetPath,
		Size:           int64(len(payload)),
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{command, marker},
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			if _, err := os.Lstat(marker); err == nil {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("execution error = %v; want context cancellation", err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionFailed || unit.PhysicalRemoved || unit.FreedBytes != 0 {
		t.Fatalf("cancelled command receipt = %+v; want failed with retained physical owner", unit)
	}
	if unit.ResidualBytes != int64(len(payload)) {
		t.Fatalf("cancelled residual = %d; want remaining payload %d", unit.ResidualBytes, len(payload))
	}
	assertPathExists(t, targetPath)
}

func TestExecutePreparedCommandRemovingOwnerThenFailingIsPartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command fixture is Unix-specific")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "command-removes-owner")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("command-removes-owner-payload")
	if err := os.WriteFile(filepath.Join(targetPath, "payload"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(t.TempDir(), "remove-then-fail")
	writeJSONReceiptExecutable(t, command, "#!/bin/sh\nrm -rf \"$1\"\nexit 7\n")
	target := types.DebrisInfo{
		ID:             "command-removes-owner",
		Tool:           types.ToolBuildCache,
		Category:       types.CategoryBuildCache,
		Path:           targetPath,
		Size:           int64(len(payload)),
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{command, targetPath},
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	targetIDKey := cleanJSONReceiptItemKey(target)
	execution, err := executePreparedCleanTargets(
		context.Background(),
		prepareCleanExecutionWithSafety(context.Background(), selection, runtime),
		quietActiveWorktreeExecutionOptions(),
	)
	if err == nil {
		t.Fatal("command failure unexpectedly succeeded")
	}
	unit := singleExecutionUnit(t, execution)
	if unit.State != cleanExecutionPartial || !unit.PhysicalRemoved || !unit.MutationAttempted || unit.FreedBytes != target.Size {
		t.Fatalf("owner-removed command failure = %+v; want partial physical removal", unit)
	}
	jsonReceipt := cleanJSONReceipt{PhysicalTargets: []cleanJSONReceiptPhysicalTarget{{
		ID: "target-1", State: cleanJSONReceiptPending, Bytes: target.Size,
	}}}
	if err := applyCleanJSONExecutionReceipt(&jsonReceipt, map[string]string{targetIDKey: "target-1"}, execution); err != nil {
		t.Fatal(err)
	}
	finalized, finalizeErr := finishCleanJSONReceipt(jsonReceipt, err)
	if finalizeErr == nil || finalized.Status != cleanJSONReceiptPartialFailure || finalized.Totals.Requested != 1 ||
		finalized.Totals.Partial != 1 || finalized.Totals.FreedBytes != target.Size {
		t.Fatalf("owner-removed JSON receipt = %+v error=%v", finalized, finalizeErr)
	}
}

func TestExecutePreparedExternallyRemovedBeforeBarrierDoesNotClaimPartial(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "removed-before-barrier")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := types.DebrisInfo{
		ID:             "removed-before-barrier",
		Tool:           types.ToolBuildCache,
		Category:       types.CategoryBuildCache,
		Path:           targetPath,
		Size:           30,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"definitely-missing-aibris-cleaner"},
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
	if err := os.RemoveAll(targetPath); err != nil {
		t.Fatal(err)
	}
	execution, err := executePreparedCleanTargets(
		context.Background(), prepared, quietActiveWorktreeExecutionOptions(),
	)
	if err == nil {
		t.Fatal("pre-mutation disappearance unexpectedly succeeded")
	}
	unit := singleExecutionUnit(t, execution)
	if unit.State != cleanExecutionFailed || !unit.PhysicalRemoved || unit.MutationAttempted || unit.FreedBytes != 0 {
		t.Fatalf("pre-mutation disappearance receipt = %+v; want failed with zero credited bytes", unit)
	}
}

func TestExecutePreparedCommandRemovingOwnerThenCancelledIsPartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command fixture is Unix-specific")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "command-removes-then-cancels")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("command-removes-then-cancels-payload")
	if err := os.WriteFile(filepath.Join(targetPath, "payload"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "command-owner-removed")
	command := filepath.Join(t.TempDir(), "remove-then-wait")
	writeJSONReceiptExecutable(t, command, "#!/bin/sh\nrm -rf \"$1\"\ntouch \"$2\"\nsleep 5\n")
	target := types.DebrisInfo{
		ID:             "command-removes-then-cancels",
		Tool:           types.ToolBuildCache,
		Category:       types.CategoryBuildCache,
		Path:           targetPath,
		Size:           int64(len(payload)),
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{command, targetPath, marker},
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			if _, err := os.Lstat(marker); err == nil {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	execution, err := executePreparedCleanTargets(
		ctx,
		prepareCleanExecutionWithSafety(context.Background(), selection, runtime),
		quietActiveWorktreeExecutionOptions(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("execution error = %v; want context cancellation", err)
	}
	unit := singleExecutionUnit(t, execution)
	if unit.State != cleanExecutionPartial || !unit.PhysicalRemoved || !unit.MutationAttempted || unit.FreedBytes != target.Size {
		t.Fatalf("owner-removed command cancellation = %+v; want partial physical removal", unit)
	}
}

func TestExecutePreparedMissingCommandRecordsFallbackPathRemoval(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "command-fallback")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := types.DebrisInfo{
		ID:             "command-fallback",
		Tool:           types.ToolBuildCache,
		Category:       types.CategoryBuildCache,
		Path:           targetPath,
		Size:           19,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"definitely-missing-aibris-cleaner"},
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executePreparedCleanTargets(
		context.Background(),
		prepareCleanExecutionWithSafety(context.Background(), selection, runtime),
		quietActiveWorktreeExecutionOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionRemoved || !unit.PhysicalRemoved || !unit.CommandFallbackPathRemoval {
		t.Fatalf("fallback command receipt = %+v; want physically removed fallback", unit)
	}
	if !pathDoesNotExist(targetPath) {
		t.Fatalf("fallback path %q still exists", targetPath)
	}
}

func TestExecuteActiveWorktreePreflightCancellationRecordsComponentBlocker(t *testing.T) {
	home, _, worktree := newExecutorWorktree(t, "preflight-cancelled")
	testutil.SetHome(t, home)
	item := executorWorktreeItem(worktree, 902)
	selected := buildExecutorUnit(t, item)
	obligationPath := filepath.Join(worktree, "agent-state")
	component := &cleanupOverlapComponent{
		CanonicalPath: selected.TargetPath,
		Owner:         item,
		Obligations: []cleaner.AgentStateObligation{{
			Tool:       types.ToolClaude,
			EntryPath:  obligationPath,
			ProviderID: "claude",
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	receipt, err := executeActiveWorktreeUnit(
		ctx,
		item,
		component,
		selected,
		nil,
		nil,
		defaultActiveWorktreeExecutionOptions(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("executeActiveWorktreeUnit() error = %v; want context cancellation", err)
	}
	if receipt.Component != component ||
		receipt.State != cleanExecutionFailed ||
		receipt.PhysicalRemoved ||
		receipt.FreedBytes != 0 ||
		receipt.BlockingPath != item.Path ||
		!strings.Contains(receipt.BlockingReason, context.Canceled.Error()) ||
		len(receipt.Obligations) != 1 ||
		receipt.Obligations[0].EntryPath != obligationPath ||
		receipt.Obligations[0].State != cleaner.AgentStateRevalidationNotAttempted {
		t.Fatalf("cancelled preflight receipt = %+v; want component blocker and pending obligation", receipt)
	}
	assertPathExists(t, worktree)
}

func TestExecuteActiveWorktreeMissingUnitPreservesComponentLineage(t *testing.T) {
	home, _, worktree := newExecutorWorktree(t, "missing-unit")
	testutil.SetHome(t, home)
	item := executorWorktreeItem(worktree, 903)
	selected := buildExecutorUnit(t, item)
	target := preparedExecutorTarget(t, item, selected)
	target.ActiveUnit = nil
	obligationPath := filepath.Join(worktree, "agent-state")
	target.Component = &cleanupOverlapComponent{
		CanonicalPath: selected.TargetPath,
		Owner:         item,
		Obligations: []cleaner.AgentStateObligation{{
			Tool:       types.ToolClaude,
			EntryPath:  obligationPath,
			ProviderID: "claude",
		}},
	}

	receipt, err := executePreparedCleanTargets(
		context.Background(),
		[]preparedCleanTarget{target},
		defaultActiveWorktreeExecutionOptions(),
	)
	if err == nil || !strings.Contains(err.Error(), "active worktree evidence unavailable") {
		t.Fatalf("executePreparedCleanTargets() error = %v; want missing evidence failure", err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.Component != target.Component ||
		unit.State != cleanExecutionFailed ||
		unit.PhysicalRemoved ||
		unit.FreedBytes != 0 ||
		unit.BlockingPath != item.Path ||
		!strings.Contains(unit.BlockingReason, "active worktree evidence unavailable") ||
		len(unit.Obligations) != 1 ||
		unit.Obligations[0].EntryPath != obligationPath ||
		unit.Obligations[0].State != cleaner.AgentStateRevalidationNotAttempted {
		t.Fatalf("missing-unit receipt = %+v; want component blocker and pending obligation", unit)
	}
	assertPathExists(t, worktree)
}

func TestExecuteActiveWorktreeReportsPartialMultiMemberResultWithoutFreedBytes(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 1024)
	selected := buildExecutorUnit(t, item)
	opts := defaultActiveWorktreeExecutionOptions()
	realRemove := opts.removeWorktree
	opts.removeWorktree = func(ctx context.Context, repositoryID, worktreePath string) error {
		if worktreePath == second {
			return errors.New("injected second-member failure")
		}
		return realRemove(ctx, repositoryID, worktreePath)
	}

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{preparedExecutorTarget(t, item, selected)}, opts)
	if err == nil {
		t.Fatal("executePreparedCleanTargets() error = nil; want partial failure")
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionPartial || unit.PhysicalRemoved || unit.FreedBytes != 0 || receipt.FreedBytes != 0 {
		t.Errorf("unit = %+v, total freed=%d; want partial with zero freed bytes", unit, receipt.FreedBytes)
	}
	if len(unit.Members) != 2 || !unit.Members[0].Removed || unit.Members[1].Removed || unit.Members[1].Error == "" {
		t.Errorf("member receipts = %+v; want first removed and second failed", unit.Members)
	}
	if !pathDoesNotExist(first) {
		t.Errorf("first member %q still exists", first)
	}
	assertPathExists(t, target)
	assertPathExists(t, second)
	assertRepositoryDoesNotListWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)

	output := captureOutput(func() {
		printWorktreeExecutionReceipts(receipt)
	})
	for _, want := range []string{"worktree execution receipt", "unit      partial", "member  removed", "member  not removed", "physical-removed false   freed 0 B"} {
		if !strings.Contains(output, want) {
			t.Errorf("partial receipt missing %q:\n%s", want, output)
		}
	}
}

func TestExecuteActiveWorktreePreservesPartialReceiptWhenBarrierFailsBetweenMembers(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 1024)
	selected := buildExecutorUnit(t, item)
	opts := defaultActiveWorktreeExecutionOptions()
	realRemove := opts.removeWorktree
	opts.removeWorktree = realRemove
	prepared := preparedExecutorTarget(t, item, selected)
	refreshCalls := 0
	prepared.MutationSafety.runtime.Refresh = func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
		refreshCalls++
		if refreshCalls >= 3 {
			return cleaner.OverlapSafetyEvidence{}, errors.New("injected second-member safety refresh failure")
		}
		return cleaner.OverlapSafetyEvidence{Complete: true}, nil
	}

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{prepared}, opts)
	if err == nil || !strings.Contains(err.Error(), "second-member safety refresh failure") {
		t.Fatalf("between-member barrier error = %v", err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionPartial || unit.PhysicalRemoved || unit.FreedBytes != 0 ||
		len(unit.Members) != 2 || !unit.Members[0].Removed || unit.Members[1].Removed {
		t.Fatalf("between-member cancellation receipt = %+v", unit)
	}
	if !pathDoesNotExist(first) || pathDoesNotExist(second) {
		t.Fatalf("member mutation state first=%t second=%t; want only first removed", pathDoesNotExist(first), pathDoesNotExist(second))
	}
	assertRepositoryDoesNotListWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)
}

func TestExecuteActiveWorktreeOwnerRemovedBeforeFirstMemberMutationDoesNotCreditBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("external worktree-container removal fixture is not portable to Windows")
	}
	home, repository, target, _, _ := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 1024)
	selected := buildExecutorUnit(t, item)
	prepared := preparedExecutorTarget(t, item, selected)
	refreshCalls := 0
	prepared.MutationSafety.runtime.Refresh = func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
		refreshCalls++
		if refreshCalls == 2 {
			if err := os.RemoveAll(target); err != nil {
				t.Fatalf("removing owner before first member mutation: %v", err)
			}
		}
		return cleaner.OverlapSafetyEvidence{Complete: true}, nil
	}
	removeCalls := 0
	opts := defaultActiveWorktreeExecutionOptions()
	opts.removeWorktree = func(context.Context, string, string) error {
		removeCalls++
		return errors.New("unexpected worktree mutation")
	}

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{prepared}, opts)
	if err == nil || !strings.Contains(err.Error(), "pre-mutation safety barrier") {
		t.Fatalf("pre-mutation disappearance error = %v", err)
	}
	unit := singleExecutionUnit(t, receipt)
	if removeCalls != 0 || unit.State != cleanExecutionFailed || !unit.PhysicalRemoved ||
		unit.MutationAttempted || unit.FreedBytes != 0 || receipt.FreedBytes != 0 {
		t.Fatalf("pre-mutation disappearance receipt = %+v, calls=%d, total freed=%d; want failed observation with no mutation attribution", unit, removeCalls, receipt.FreedBytes)
	}
	assertRepositoryListsWorktree(t, repository, selected.Members[0].WorktreePath)
}

func TestExecuteActiveWorktreeCreditsVerifiedOwnerAbsenceAfterMemberMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("external worktree-container removal fixture is not portable to Windows")
	}
	home, _, target, first, _ := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 1024)
	selected := buildExecutorUnit(t, item)
	opts := defaultActiveWorktreeExecutionOptions()
	realRemove := opts.removeWorktree
	opts.removeWorktree = func(ctx context.Context, repositoryID, worktreePath string) error {
		if err := realRemove(ctx, repositoryID, worktreePath); err != nil {
			return err
		}
		return os.RemoveAll(target)
	}

	receipt, err := executePreparedCleanTargets(
		context.Background(),
		[]preparedCleanTarget{preparedExecutorTarget(t, item, selected)},
		opts,
	)
	if err == nil || !strings.Contains(err.Error(), "disappeared before removing remaining worktree members") {
		t.Fatalf("post-mutation disappearance error = %v", err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionPartial || !unit.PhysicalRemoved || !unit.MutationAttempted ||
		unit.FreedBytes <= 0 || receipt.FreedBytes != unit.FreedBytes {
		t.Fatalf("post-mutation disappearance receipt = %+v, total freed=%d; want attributed partial removal", unit, receipt.FreedBytes)
	}
	if len(unit.Members) != 2 || !unit.Members[0].Removed || unit.Members[1].Removed || !pathDoesNotExist(first) {
		t.Fatalf("post-mutation member receipt = %+v", unit.Members)
	}
}

func TestGitWorktreeRemoveArgsNeverIncludeForce(t *testing.T) {
	args := gitWorktreeRemoveArgs("/repo/.git", "/worktree")
	got := strings.Join(args, " ")
	if got != "--git-dir=/repo/.git worktree remove /worktree" {
		t.Fatalf("remove args = %q; want non-force Git worktree remove", got)
	}
	if strings.Contains(got, "--force") || strings.Contains(got, " -f") {
		t.Fatalf("remove args unexpectedly force Git removal: %q", got)
	}
}

func TestExecutePlainDirWorktreeRefusesDespiteGitdir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	target := filepath.Join(home, ".codex", "worktrees", "plain")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: "+filepath.Join(home, "missing", ".git", "worktrees", "plain")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "plain",
		Path:     target,
		Size:     77,
		Status:   types.WorktreePlain,
	}

	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
	if len(prepared) != 1 || prepared[0].ActiveUnit != nil {
		t.Fatalf("prepared = %+v; want no active unit from Scan plain-dir status", prepared)
	}

	receipt, err := executePreparedCleanTargets(context.Background(), prepared, defaultActiveWorktreeExecutionOptions())
	if err == nil {
		t.Fatal("executePreparedCleanTargets() error = nil; want plain-dir refusal")
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionFailed || unit.PhysicalRemoved || unit.MutationAttempted {
		t.Errorf("plain-dir receipt = %+v; want failed with no mutation", unit)
	}
	if pathDoesNotExist(target) {
		t.Errorf("plain-dir target %q was removed", target)
	}
}

func TestExecuteOrphanedWorktreeKeepsRawPathCleanup(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	target := filepath.Join(home, ".codex", "worktrees", "orphaned")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: "+filepath.Join(home, "missing", ".git", "worktrees", "orphaned")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "orphaned",
		Path:     target,
		Size:     77,
		Status:   types.WorktreeOrphaned,
	}

	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if err != nil {
		t.Fatal(err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionRemoved || !unit.PhysicalRemoved || unit.FreedBytes <= 0 || receipt.FreedBytes != unit.FreedBytes || len(unit.Members) != 0 {
		t.Errorf("orphaned receipt = %+v; want raw-path removal", unit)
	}
	if !pathDoesNotExist(target) {
		t.Errorf("orphaned target %q still exists", target)
	}
}

func TestExecutePreparedPathCleanupRejectsTargetChangedAfterSelection(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "changed-after-selection")
	sentinel := filepath.Join(targetPath, "sentinel")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(targetPath, old, old); err != nil {
		t.Fatal(err)
	}
	target := types.DebrisInfo{
		Tool:        types.ToolBuildCache,
		Category:    types.CategoryBuildCache,
		ID:          "changed-after-selection",
		Path:        targetPath,
		Size:        8,
		ModTime:     old,
		CleanupKind: types.CleanupRemovePath,
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithOptions(
		context.Background(),
		selection,
		runtime,
		types.PruneOptions{Age: 24 * time.Hour},
	)
	changed := time.Now()
	if err := os.Chtimes(targetPath, changed, changed); err != nil {
		t.Fatal(err)
	}

	receipt, err := executePreparedCleanTargets(
		context.Background(),
		prepared,
		defaultActiveWorktreeExecutionOptions(),
	)
	if err == nil || receipt.FreedBytes != 0 {
		t.Fatalf("error=%v, freed=%d; want changed-since-selection refusal", err, receipt.FreedBytes)
	}
	if !strings.Contains(err.Error(), "changed since cleanup selection") {
		t.Fatalf("error=%v; want actionable changed-since-selection reason", err)
	}
	if _, statErr := os.Lstat(sentinel); statErr != nil {
		t.Fatalf("changed target was removed: %v", statErr)
	}
}

func TestExecutePreparedPathCleanupRevalidatesSnapshotAfterOverlapRefresh(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "changed-during-overlap-refresh")
	sentinel := filepath.Join(targetPath, "sentinel")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("survive"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(targetPath, old, old); err != nil {
		t.Fatal(err)
	}
	target := types.DebrisInfo{
		Tool:        types.ToolBuildCache,
		Category:    types.CategoryBuildCache,
		ID:          "changed-during-overlap-refresh",
		Path:        targetPath,
		Size:        8,
		ModTime:     old,
		CleanupKind: types.CleanupRemovePath,
	}
	runtime := cleanupOverlapSafetyRuntime{
		OverlapRuntime: cleaner.OverlapRuntime{
			Initial: cleaner.OverlapSafetyEvidence{Complete: true},
			Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
				changed := time.Now()
				if err := os.Chtimes(targetPath, changed, changed); err != nil {
					return cleaner.OverlapSafetyEvidence{}, err
				}
				return cleaner.OverlapSafetyEvidence{Complete: true}, nil
			},
		},
	}
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithOptions(
		context.Background(),
		selection,
		runtime,
		types.PruneOptions{Age: 24 * time.Hour},
	)

	receipt, err := executePreparedCleanTargets(
		context.Background(),
		prepared,
		defaultActiveWorktreeExecutionOptions(),
	)
	if err == nil || receipt.FreedBytes != 0 {
		t.Fatalf("error=%v, freed=%d; want final snapshot refusal", err, receipt.FreedBytes)
	}
	if !strings.Contains(err.Error(), "changed since cleanup selection") {
		t.Fatalf("error=%v; want changed-since-selection reason", err)
	}
	if _, statErr := os.Lstat(sentinel); statErr != nil {
		t.Fatalf("target changed during overlap refresh was removed: %v", statErr)
	}
}

func TestPreparePathCleanupRejectsReplacementAfterScanEvidenceValidation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targetPath := filepath.Join(home, ".cache", "replaced-after-scan")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(targetPath, old, old); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		ID:       "replaced-after-scan",
		Path:     targetPath,
		Size:     8,
		ModTime:  old,
		// Cache adapters always record the path's own mtime; a cached entry
		// without it is refused, so the fixture has to carry it too.
		PathModTime: old,
		CleanupKind: types.CleanupRemovePath,
	}
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	cached, _, ok := readFreshLastScanCache([]string{resolvedHome})
	if !ok {
		t.Fatal("readFreshLastScanCache() rejected valid fixture")
	}
	if len(cached.Worktrees) != 1 {
		t.Fatalf("readFreshLastScanCache() items = %d; want one bound cached target",
			len(cached.Worktrees))
	}
	if err := os.Rename(targetPath, targetPath+"-original"); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(targetPath, "replacement-sentinel")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("survive"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetPath, old, old); err != nil {
		t.Fatal(err)
	}

	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, cached.Worktrees)
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithOptions(
		context.Background(),
		selection,
		runtime,
		types.PruneOptions{Age: 24 * time.Hour},
	)
	receipt, err := executePreparedCleanTargets(
		context.Background(),
		prepared,
		defaultActiveWorktreeExecutionOptions(),
	)
	if err == nil || receipt.FreedBytes != 0 {
		t.Fatalf("error=%v, freed=%d; want scan-identity refusal", err, receipt.FreedBytes)
	}
	if !strings.Contains(err.Error(), "changed since scan") {
		t.Fatalf("error=%v; want changed-since-scan reason", err)
	}
	if _, statErr := os.Lstat(sentinel); statErr != nil {
		t.Fatalf("replacement after scan evidence was removed: %v", statErr)
	}
}

func newExecutorWorktree(t *testing.T, branch string) (home, repository, worktree string) {
	t.Helper()
	home = t.TempDir()
	home, _ = cleaner.TargetPathKey(home)
	repository = filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repository)
	worktree = filepath.Join(home, ".codex", "worktrees", branch)
	if err := os.MkdirAll(filepath.Dir(worktree), 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", branch, worktree, "HEAD")
	return home, repository, worktree
}

func newExecutorMultiMemberUnit(t *testing.T) (home, repository, target, first, second string) {
	t.Helper()
	home = t.TempDir()
	home, _ = cleaner.TargetPathKey(home)
	repository = filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repository)
	target = filepath.Join(home, ".codex", "worktrees", "multi")
	first = filepath.Join(target, "a-first")
	second = filepath.Join(target, "b-second")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", "executor-first", first, "HEAD")
	runGitFixture(t, repository, "worktree", "add", "-b", "executor-second", second, "HEAD")
	return home, repository, target, first, second
}

func executorWorktreeItem(path string, size int64) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       filepath.Base(path),
		Path:     path,
		Size:     size,
		Status:   types.WorktreeActive,
	}
}

func buildExecutorUnit(t testing.TB, item types.DebrisInfo) WorktreeCleanupUnit {
	t.Helper()
	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("cleanup units = %d; want 1 (%+v)", len(units), units)
	}
	return units[0]
}

func singleExecutionUnit(t *testing.T, receipt cleanExecutionReceipt) cleanUnitExecutionReceipt {
	t.Helper()
	if len(receipt.Units) != 1 {
		t.Fatalf("execution units = %d; want 1 (%+v)", len(receipt.Units), receipt.Units)
	}
	return receipt.Units[0]
}

func assertRemovedExecutionUnit(t *testing.T, receipt cleanExecutionReceipt, wantFreed int64, worktree string) {
	t.Helper()
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionRemoved || !unit.PhysicalRemoved || unit.FreedBytes != wantFreed || receipt.FreedBytes != wantFreed {
		t.Fatalf("unit = %+v, total freed=%d; want removed and %d freed", unit, receipt.FreedBytes, wantFreed)
	}
	if len(unit.Members) != 1 || !unit.Members[0].Removed || unit.Members[0].WorktreePath != worktree || unit.Members[0].Error != "" {
		t.Errorf("member receipts = %+v; want removed %q", unit.Members, worktree)
	}
	if !pathDoesNotExist(worktree) {
		t.Errorf("worktree %q still exists", worktree)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("path %q should exist: %v", path, err)
	}
}

func assertRepositoryListsWorktree(t *testing.T, repository, worktree string) {
	t.Helper()
	output := runGitFixtureOutput(t, repository, "worktree", "list", "--porcelain")
	if !strings.Contains(output, "worktree "+worktree+"\n") {
		t.Fatalf("repository does not list %q:\n%s", worktree, output)
	}
}

func assertRepositoryDoesNotListWorktree(t *testing.T, repository, worktree string) {
	t.Helper()
	output := runGitFixtureOutput(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(output, "worktree "+worktree+"\n") {
		t.Fatalf("repository still lists %q:\n%s", worktree, output)
	}
}

func runGitFixtureOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}
