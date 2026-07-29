package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteCleanTargetsRefusesGenericParentContainingLiveAgentState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	outer := filepath.Join(home, ".cache", "generic-parent")
	protected := filepath.Join(outer, ".cursor", "projects", "live-entry")
	sentinel := filepath.Join(protected, "sentinel")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("must survive"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		ID:       "generic-parent",
		Path:     outer,
		Size:     12,
	}
	protectedEntry := types.DebrisInfo{
		Tool:           types.ToolCursor,
		Category:       types.CategoryAgentState,
		ID:             "live-entry",
		Path:           protected,
		Classification: types.EntryClassLive,
	}
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{protectedEntry}, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Targets) != 0 || len(selection.Plan.Components) != 1 ||
		selection.Plan.Components[0].Refusal == nil {
		t.Fatalf("selection = %+v; want one protected refusal and no executable targets", selection)
	}

	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if err != nil {
		t.Fatalf("executing empty protected selection: %v", err)
	}
	if receipt.FreedBytes != 0 {
		t.Fatalf("freed bytes = %d; want 0", receipt.FreedBytes)
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("protected nested sentinel was removed: %v", statErr)
	}
}

func TestOverlapSafetyPreparationRefusesProtectedShapesBeforeExecution(t *testing.T) {
	tests := []struct {
		name           string
		classification types.EntryClass
		entryRelative  string
		targetRelative string
	}{
		{
			name:           "live parent",
			classification: types.EntryClassLive,
			entryRelative:  "component",
			targetRelative: filepath.Join("component", "deep", "node_modules"),
		},
		{
			name:           "undetermined parent",
			classification: types.EntryClassUndetermined,
			entryRelative:  "component",
			targetRelative: filepath.Join("component", "deep", "node_modules"),
		},
		{
			name:           "live child",
			classification: types.EntryClassLive,
			entryRelative:  filepath.Join("component", "deep", "agent"),
			targetRelative: "component",
		},
		{
			name:           "undetermined child",
			classification: types.EntryClassUndetermined,
			entryRelative:  filepath.Join("component", "deep", "agent"),
			targetRelative: "component",
		},
		{
			name:           "exact path",
			classification: types.EntryClassLive,
			entryRelative:  "component",
			targetRelative: "component",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			base := filepath.Join(home, ".cache", tt.name)
			targetPath := filepath.Join(base, tt.targetRelative)
			entryPath := filepath.Join(base, tt.entryRelative)
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(entryPath, 0o755); err != nil {
				t.Fatal(err)
			}
			targetSentinel := filepath.Join(targetPath, "target-sentinel")
			entrySentinel := filepath.Join(entryPath, "entry-sentinel")
			for _, sentinel := range []string{targetSentinel, entrySentinel} {
				if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			entry := overlapCmdAgentStateItem(entryPath, tt.classification)
			target := overlapCmdTarget(targetPath, 19)
			runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{entry}, nil)
			selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := executeCleanTargets(context.Background(), selection, runtime)
			if err != nil {
				t.Fatalf("executing refused selection: %v", err)
			}
			if len(selection.Targets) != 0 || receipt.FreedBytes != 0 {
				t.Fatalf("targets=%d, freed=%d; want refusal with zero bytes", len(selection.Targets), receipt.FreedBytes)
			}
			assertOverlapSentinelsSurvive(t, targetSentinel, entrySentinel)
		})
	}
}

func TestExecuteOverlapSafetyRevalidatesNestedOrphanBeforeRemovingOuterTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outer := filepath.Join(home, ".cache", "orphan-success")
	entry := filepath.Join(outer, "nested", "orphan")
	sentinel := filepath.Join(entry, "sentinel")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("remove after validation"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(_ context.Context, path string) (types.EntryClass, error) {
			calls++
			if filepath.Base(path) != "orphan" {
				t.Fatalf("revalidated path = %q; want exact nested entry", path)
			}
			return types.EntryClassOrphaned, nil
		},
	})
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{
		overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
	}, lookup)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
		overlapCmdTarget(outer, 31),
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("revalidation calls = %d; want 1", calls)
	}
	unit := singleExecutionUnit(t, receipt)
	if !unit.PhysicalRemoved || unit.FreedBytes != 31 || receipt.FreedBytes != 31 {
		t.Fatalf("receipt = %+v; want one 31-byte physical removal", receipt)
	}
	if _, statErr := os.Lstat(outer); !os.IsNotExist(statErr) {
		t.Fatalf("outer target survived successful barrier: %v", statErr)
	}
}

func TestExecuteOverlapSafetyLateObligationFailureIsAtomicAndUnrelatedTargetContinues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	blocked := filepath.Join(home, ".cache", "blocked-component")
	first := filepath.Join(blocked, "agent", "first")
	second := filepath.Join(blocked, "agent", "second")
	unrelated := filepath.Join(home, ".cache", "unrelated")
	for _, path := range []string{first, second, unrelated} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	firstSentinel := filepath.Join(first, "sentinel")
	secondSentinel := filepath.Join(second, "sentinel")
	unrelatedSentinel := filepath.Join(unrelated, "sentinel")
	for _, sentinel := range []string{firstSentinel, secondSentinel, unrelatedSentinel} {
		if err := os.WriteFile(sentinel, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	lateFailure := errors.New("late child failed")
	var calls []string
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(_ context.Context, path string) (types.EntryClass, error) {
			calls = append(calls, filepath.Base(path))
			if filepath.Base(path) == "second" {
				return "", lateFailure
			}
			return types.EntryClassOrphaned, nil
		},
	})
	inventory := []types.DebrisInfo{
		overlapCmdAgentStateItem(first, types.EntryClassOrphaned),
		overlapCmdAgentStateItem(second, types.EntryClassOrphaned),
	}
	runtime := staticOverlapSafetyRuntime(inventory, lookup)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
		overlapCmdTarget(blocked, 41),
		overlapCmdTarget(unrelated, 7),
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if !errors.Is(err, lateFailure) {
		t.Fatalf("executeCleanTargets() error = %v; want late child failure", err)
	}
	if strings.Join(calls, ",") != "first,second" {
		t.Fatalf("revalidation calls = %v; want stable all-obligation order", calls)
	}
	if receipt.FreedBytes != 7 {
		t.Fatalf("freed bytes = %d; want unrelated 7 bytes only", receipt.FreedBytes)
	}
	if len(receipt.Units) != 2 || receipt.Units[0].FreedBytes != 0 ||
		receipt.Units[0].PhysicalRemoved || !receipt.Units[1].PhysicalRemoved {
		t.Fatalf("receipt = %+v; want blocked then unrelated removal", receipt)
	}
	assertOverlapSentinelsSurvive(t, firstSentinel, secondSentinel)
	if _, statErr := os.Lstat(unrelatedSentinel); !os.IsNotExist(statErr) {
		t.Fatalf("unrelated target did not continue to removal: %v", statErr)
	}
}

func TestExecuteOverlapSafetyRefreshCatchesClassificationDriftAndNewEntries(t *testing.T) {
	tests := []struct {
		name       string
		initial    func(string) []types.DebrisInfo
		refreshed  func(string) []types.DebrisInfo
		wantRemove bool
	}{
		{
			name: "orphan becomes live",
			initial: func(entry string) []types.DebrisInfo {
				return []types.DebrisInfo{overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)}
			},
			refreshed: func(entry string) []types.DebrisInfo {
				return []types.DebrisInfo{overlapCmdAgentStateItem(entry, types.EntryClassLive)}
			},
		},
		{
			name: "orphan becomes undetermined",
			initial: func(entry string) []types.DebrisInfo {
				return []types.DebrisInfo{overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)}
			},
			refreshed: func(entry string) []types.DebrisInfo {
				return []types.DebrisInfo{overlapCmdAgentStateItem(entry, types.EntryClassUndetermined)}
			},
		},
		{
			name: "new live entry",
			initial: func(string) []types.DebrisInfo {
				return nil
			},
			refreshed: func(entry string) []types.DebrisInfo {
				return []types.DebrisInfo{overlapCmdAgentStateItem(entry, types.EntryClassLive)}
			},
		},
		{
			name: "new orphan obligation incorporated",
			initial: func(string) []types.DebrisInfo {
				return nil
			},
			refreshed: func(entry string) []types.DebrisInfo {
				return []types.DebrisInfo{overlapCmdAgentStateItem(entry, types.EntryClassOrphaned)}
			},
			wantRemove: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			outer := filepath.Join(home, ".cache", strings.ReplaceAll(tt.name, " ", "-"))
			entry := filepath.Join(outer, "new-agent-entry")
			sentinel := filepath.Join(entry, "sentinel")
			if err := os.MkdirAll(entry, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sentinel, []byte("fixture"), 0o644); err != nil {
				t.Fatal(err)
			}
			revalidationCalls := 0
			lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
				types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
					revalidationCalls++
					return types.EntryClassOrphaned, nil
				},
			})
			initial := cleaner.OverlapSafetyEvidence{Items: tt.initial(entry), Complete: true}
			runtime := cleanupOverlapSafetyRuntime{
				Initial: initial,
				Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
					return cleaner.OverlapSafetyEvidence{Items: tt.refreshed(entry), Complete: true}, nil
				},
				Lookup: lookup,
			}
			selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
				overlapCmdTarget(outer, 23),
			})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := executeCleanTargets(context.Background(), selection, runtime)
			if tt.wantRemove {
				if err != nil {
					t.Fatal(err)
				}
				if receipt.FreedBytes != 23 || revalidationCalls != 1 {
					t.Fatalf("freed=%d, revalidations=%d; want 23 and 1", receipt.FreedBytes, revalidationCalls)
				}
				if _, statErr := os.Lstat(outer); !os.IsNotExist(statErr) {
					t.Fatalf("outer target survived successful new obligation: %v", statErr)
				}
				return
			}
			if err == nil || receipt.FreedBytes != 0 {
				t.Fatalf("error=%v, freed=%d; want refreshed refusal with zero bytes", err, receipt.FreedBytes)
			}
			if revalidationCalls != 0 {
				t.Fatalf("revalidator ran after refreshed hard lock: %d", revalidationCalls)
			}
			assertOverlapSentinelsSurvive(t, sentinel)
		})
	}
}

func TestExecuteOverlapSafetyRefusesSymlinkRetargetAndIncompleteRefresh(t *testing.T) {
	t.Run("canonicalization error fails closed during preparation", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		target := filepath.Join(home, ".cache", "canonicalization-error")
		sentinel := filepath.Join(target, "sentinel")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
			t.Fatal(err)
		}
		entry := makeOverlapCmdSymlinkDepthError(
			t,
			filepath.Join(home, ".agent-state-aliases"),
			target,
		)
		runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{
			overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
		}, nil)
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
			overlapCmdTarget(target, 5),
		})
		if err != nil {
			t.Fatal(err)
		}
		component := selection.Plan.Components[0]
		if len(selection.Targets) != 0 ||
			component.Refusal == nil ||
			component.Refusal.Reason != cleaner.OverlapSafetyAmbiguousIdentity ||
			!strings.Contains(component.Refusal.Detail, "too many symlinks") {
			t.Fatalf("selection = %+v; want canonicalization-error refusal", selection)
		}

		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if err != nil || receipt.FreedBytes != 0 {
			t.Fatalf("error=%v, freed=%d; want preparation refusal with zero bytes",
				err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t, target, sentinel)
	})

	t.Run("unresolvable path below intermediate symlink", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		target := filepath.Join(home, ".cache", "real")
		sentinel := filepath.Join(target, "sentinel")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(home, ".cache", "alias")
		if err := os.Symlink(target, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		entry := filepath.Join(alias, "missing")
		runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{
			overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
		}, nil)
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
			overlapCmdTarget(target, 5),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(selection.Targets) != 0 || len(selection.Plan.Components) != 1 ||
			selection.Plan.Components[0].Refusal == nil ||
			selection.Plan.Components[0].Refusal.Reason != cleaner.OverlapSafetyAmbiguousIdentity {
			t.Fatalf("selection = %+v; want ambiguous identity refusal", selection)
		}
		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if err != nil || receipt.FreedBytes != 0 {
			t.Fatalf("error=%v, freed=%d; want refusal with zero bytes", err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t, sentinel)
	})

	t.Run("symlink retarget", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		first := filepath.Join(home, ".cache", "first")
		second := filepath.Join(home, ".cache", "second")
		for _, path := range []string{first, second} {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "sentinel"), []byte("survive"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		alias := filepath.Join(home, ".cache", "alias")
		if err := os.Symlink(first, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		runtime := staticOverlapSafetyRuntime(nil, nil)
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
			overlapCmdTarget(alias, 5),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(second, alias); err != nil {
			t.Fatal(err)
		}
		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if err == nil || receipt.FreedBytes != 0 {
			t.Fatalf("error=%v, freed=%d; want retarget refusal", err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t,
			filepath.Join(first, "sentinel"),
			filepath.Join(second, "sentinel"),
		)
	})

	t.Run("incomplete refresh", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		target := filepath.Join(home, ".cache", "partial-refresh")
		sentinel := filepath.Join(target, "sentinel")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
			t.Fatal(err)
		}
		runtime := cleanupOverlapSafetyRuntime{
			Initial: cleaner.OverlapSafetyEvidence{Complete: true},
			Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
				return cleaner.OverlapSafetyEvidence{
					ProviderErrors: []types.ScanProviderError{{
						Tool:    types.ToolCursor,
						Message: "injected",
					}},
				}, nil
			},
		}
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
			overlapCmdTarget(target, 5),
		})
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if !errors.Is(err, cleaner.ErrIncompleteOverlapSafetyEvidence) || receipt.FreedBytes != 0 {
			t.Fatalf("error=%v, freed=%d; want incomplete refresh refusal", err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t, sentinel)
	})
}

func TestExecuteOverlapSafetyRefusesCommandBeforeExecutionAndMissingFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, ".cache", "command-overlap")
	entry := filepath.Join(target, "agent")
	sentinel := filepath.Join(entry, "sentinel")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
			t.Fatal("command overlap must refuse before revalidation")
			return "", nil
		},
	})
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{
		overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
	}, lookup)
	for _, command := range [][]string{
		{"definitely-missing-overlap-command"},
		{"sh", "-c", "touch " + filepath.Join(home, "command-ran")},
	} {
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{{
			Tool:           types.ToolBuildCache,
			Category:       types.CategoryBuildCache,
			ID:             "command-overlap",
			Path:           target,
			Size:           9,
			CleanupKind:    types.CleanupCommand,
			CleanupCommand: command,
		}})
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := executeCleanTargets(context.Background(), selection, runtime)
		if err != nil || len(selection.Targets) != 0 || receipt.FreedBytes != 0 {
			t.Fatalf("command=%v, targets=%d, error=%v, freed=%d; want planning refusal",
				command, len(selection.Targets), err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t, sentinel)
		if _, statErr := os.Lstat(filepath.Join(home, "command-ran")); !os.IsNotExist(statErr) {
			t.Fatalf("cleanup command ran despite overlap: %v", statErr)
		}
	}
}

func TestOverlapSafetyRefusesMissingOrAmbiguousRevalidatorBeforeExecution(t *testing.T) {
	tests := []struct {
		name   string
		lookup cleaner.AgentStateRevalidatorLookup
	}{
		{name: "missing"},
		{
			name: "ambiguous",
			lookup: func(tool types.Tool) (adapter.AgentStateRevalidatorRegistration, error) {
				return adapter.AgentStateRevalidatorRegistration{},
					errors.New("ambiguous duplicate revalidator for " + string(tool))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			target := filepath.Join(home, ".cache", "registration-"+tt.name)
			entry := filepath.Join(target, "orphan")
			sentinel := filepath.Join(entry, "sentinel")
			if err := os.MkdirAll(entry, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
				t.Fatal(err)
			}
			runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{
				overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
			}, tt.lookup)
			selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
				overlapCmdTarget(target, 6),
			})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := executeCleanTargets(context.Background(), selection, runtime)
			if err != nil || len(selection.Targets) != 0 || receipt.FreedBytes != 0 {
				t.Fatalf("targets=%d, error=%v, freed=%d; want planning refusal",
					len(selection.Targets), err, receipt.FreedBytes)
			}
			assertOverlapSentinelsSurvive(t, sentinel)
		})
	}
}

func TestExecuteOverlapSafetyRefreshBlocksNewCommandOverlapBeforeCommandRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, ".cache", "command-refresh")
	entry := filepath.Join(target, "agent")
	sentinel := filepath.Join(entry, "sentinel")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "command-ran")
	runtime := cleanupOverlapSafetyRuntime{
		Initial: cleaner.OverlapSafetyEvidence{Complete: true},
		Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
			return cleaner.OverlapSafetyEvidence{
				Items: []types.DebrisInfo{
					overlapCmdAgentStateItem(entry, types.EntryClassLive),
				},
				Complete: true,
			}, nil
		},
	}
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{{
		Tool:           types.ToolBuildCache,
		Category:       types.CategoryBuildCache,
		ID:             "command-refresh",
		Path:           target,
		Size:           13,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"sh", "-c", "touch " + marker},
	}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if err == nil || receipt.FreedBytes != 0 {
		t.Fatalf("error=%v, freed=%d; want refreshed command refusal", err, receipt.FreedBytes)
	}
	assertOverlapSentinelsSurvive(t, sentinel)
	if _, statErr := os.Lstat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("command ran before refreshed barrier: %v", statErr)
	}
}

func TestOverlapSafetySurvivesSelectorAndForceFiltering(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	targetPath := filepath.Join(home, ".cache", "selector-force")
	entryPath := filepath.Join(targetPath, "agent")
	sentinel := filepath.Join(entryPath, "sentinel")
	if err := os.MkdirAll(entryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := overlapCmdTarget(targetPath, 17)
	target.ModTime = time.Now().Add(-48 * time.Hour)
	entry := overlapCmdAgentStateItem(entryPath, types.EntryClassLive)
	inventory := []types.DebrisInfo{entry, target}
	filtered := cleaner.Filter(inventory, types.PruneOptions{
		Age:        24 * time.Hour,
		Categories: []types.Category{types.CategoryBuildCache},
		Tools:      []types.Tool{types.ToolBuildCache},
		Force:      true,
		Risky:      true,
	})
	if len(filtered) != 1 || filtered[0].Path != targetPath {
		t.Fatalf("filtered = %+v; want only generic target", filtered)
	}
	runtime := staticOverlapSafetyRuntime(inventory, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, filtered)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executeCleanTargets(context.Background(), selection, runtime)
	if err != nil || len(selection.Targets) != 0 || receipt.FreedBytes != 0 {
		t.Fatalf("targets=%d, error=%v, freed=%d; selector/force bypassed safety",
			len(selection.Targets), err, receipt.FreedBytes)
	}
	assertOverlapSentinelsSurvive(t, sentinel)
}

func TestOverlapSafetyAuditReasonsNeverFallBackToMissingPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	targetPath := filepath.Join(home, ".cache", "audit-overlap")
	entryPath := filepath.Join(targetPath, "agent")
	if err := os.MkdirAll(entryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := overlapCmdTarget(targetPath, 17)
	target.ModTime = time.Now().Add(-48 * time.Hour)
	entry := overlapCmdAgentStateItem(entryPath, types.EntryClassLive)
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{entry}, nil)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{target})
	if err != nil {
		t.Fatal(err)
	}
	reason := cleanAuditBlockReason(
		target,
		types.PruneOptions{Age: 24 * time.Hour},
		time.Now(),
		newCleanAuditTargetSet(selection.Targets),
		selection.Protections,
	)
	if reason != cleanReasonProtectedAgentStateDescendant {
		t.Fatalf("audit reason = %q; want protected descendant, not missing path", reason)
	}

	entry.Classification = types.EntryClassOrphaned
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
			return types.EntryClassOrphaned, nil
		},
	})
	runtime = staticOverlapSafetyRuntime([]types.DebrisInfo{entry}, lookup)
	commandTarget := target
	commandTarget.CleanupKind = types.CleanupCommand
	commandTarget.CleanupCommand = []string{"missing"}
	selection, err = applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{commandTarget})
	if err != nil {
		t.Fatal(err)
	}
	reason = cleanAuditBlockReason(
		entry,
		types.PruneOptions{Age: 24 * time.Hour},
		time.Now(),
		newCleanAuditTargetSet(selection.Targets),
		selection.Protections,
	)
	if reason != cleanReasonCommandOverlap {
		t.Fatalf("nested orphan audit reason = %q; want command overlap, not missing path", reason)
	}
}

func TestExecuteOverlapSafetyCancellationAndInteractiveConfirmationStayPreMutation(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		target := filepath.Join(home, ".cache", "cancelled")
		sentinel := filepath.Join(target, "sentinel")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
			t.Fatal(err)
		}
		runtime := staticOverlapSafetyRuntime(nil, nil)
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
			overlapCmdTarget(target, 8),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		receipt, err := executeCleanTargets(ctx, selection, runtime)
		if !errors.Is(err, context.Canceled) || receipt.FreedBytes != 0 {
			t.Fatalf("error=%v, freed=%d; want cancellation refusal", err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t, sentinel)
	})

	t.Run("interactive refresh after yes", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		target := filepath.Join(home, ".cache", "interactive")
		entry := filepath.Join(target, "new-agent")
		sentinel := filepath.Join(entry, "sentinel")
		if err := os.MkdirAll(entry, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
			t.Fatal(err)
		}
		runtime := cleanupOverlapSafetyRuntime{
			Initial: cleaner.OverlapSafetyEvidence{Complete: true},
			Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
				return cleaner.OverlapSafetyEvidence{
					Items: []types.DebrisInfo{
						overlapCmdAgentStateItem(entry, types.EntryClassLive),
					},
					Complete: true,
				}, nil
			},
		}
		selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{
			overlapCmdTarget(target, 8),
		})
		if err != nil {
			t.Fatal(err)
		}
		prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
		input, err := os.CreateTemp(t.TempDir(), "interactive-input")
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		if _, err := input.WriteString("y\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := input.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		previousStdin := os.Stdin
		os.Stdin = input
		defer func() { os.Stdin = previousStdin }()

		receipt, err := interactiveClean(context.Background(), prepared)
		if err == nil || receipt.FreedBytes != 0 {
			t.Fatalf("error=%v, freed=%d; want post-confirmation refresh refusal", err, receipt.FreedBytes)
		}
		assertOverlapSentinelsSurvive(t, sentinel)
	})
}

func TestExecuteActiveWorktreeOverlapBarrierRunsBeforeGitMutation(t *testing.T) {
	home, repository, worktree := newExecutorWorktree(t, "nested-agent-state")
	t.Setenv("HOME", home)
	entry := filepath.Dir(worktree)
	sentinel := filepath.Join(entry, "safety-sentinel")
	if err := os.WriteFile(sentinel, []byte("survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := executorWorktreeItem(worktree, 101)
	revalidationErr := errors.New("nested worktree obligation failed")
	lookup := overlapCmdLookup(map[types.Tool]overlapCmdRevalidator{
		types.ToolClaude: func(context.Context, string) (types.EntryClass, error) {
			return "", revalidationErr
		},
	})
	runtime := staticOverlapSafetyRuntime([]types.DebrisInfo{
		overlapCmdAgentStateItem(entry, types.EntryClassOrphaned),
	}, lookup)
	selection, err := applyCleanupOverlapSafety(context.Background(), runtime, []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithSafety(context.Background(), selection, runtime)
	opts := defaultActiveWorktreeExecutionOptions()
	removeCalls := 0
	opts.removeWorktree = func(context.Context, string, string) error {
		removeCalls++
		return nil
	}
	receipt, err := executePreparedCleanTargets(context.Background(), prepared, opts)
	if !errors.Is(err, revalidationErr) {
		t.Fatalf("error=%v; want nested revalidation failure", err)
	}
	if removeCalls != 0 || receipt.FreedBytes != 0 {
		t.Fatalf("git calls=%d, freed=%d; mutation preceded barrier", removeCalls, receipt.FreedBytes)
	}
	assertOverlapSentinelsSurvive(t, sentinel)
	assertRepositoryListsWorktree(t, repository, worktree)
}

type overlapCmdRevalidator func(context.Context, string) (types.EntryClass, error)

func (fn overlapCmdRevalidator) RevalidateAgentState(ctx context.Context, path string) (types.EntryClass, error) {
	return fn(ctx, path)
}

func overlapCmdLookup(
	revalidators map[types.Tool]overlapCmdRevalidator,
) cleaner.AgentStateRevalidatorLookup {
	return func(tool types.Tool) (adapter.AgentStateRevalidatorRegistration, error) {
		revalidator, ok := revalidators[tool]
		if !ok {
			return adapter.AgentStateRevalidatorRegistration{},
				errors.New("test revalidator missing")
		}
		return adapter.AgentStateRevalidatorRegistration{
			Tool:        tool,
			ProviderID:  "cmd-test-provider:" + string(tool),
			Revalidator: revalidator,
		}, nil
	}
}

func overlapCmdAgentStateItem(path string, classification types.EntryClass) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             filepath.Base(path),
		Path:           path,
		Classification: classification,
	}
}

func overlapCmdTarget(path string, size int64) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		ID:       filepath.Base(path),
		Path:     path,
		Size:     size,
	}
}

func assertOverlapSentinelsSurvive(t *testing.T, sentinels ...string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if _, err := os.Lstat(sentinel); err != nil {
			t.Fatalf("sentinel %q did not survive refusal: %v", sentinel, err)
		}
	}
}

func staticOverlapSafetyRuntime(
	items []types.DebrisInfo,
	lookup cleaner.AgentStateRevalidatorLookup,
) cleanupOverlapSafetyRuntime {
	evidence := cleaner.OverlapSafetyEvidence{
		Items:    append([]types.DebrisInfo(nil), items...),
		Complete: true,
	}
	return cleanupOverlapSafetyRuntime{
		Initial: evidence,
		Refresh: func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
			return evidence, nil
		},
		Lookup: lookup,
	}
}

func makeOverlapCmdSymlinkDepthError(t *testing.T, aliasesRoot, target string) string {
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
