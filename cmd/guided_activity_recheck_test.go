package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

func guidedActivityFixture(t *testing.T, orca bool) (string, []types.DebrisInfo, []string) {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	old := time.Now().Add(-30 * 24 * time.Hour)
	t.Setenv("GIT_AUTHOR_DATE", old.Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", old.Format(time.RFC3339))
	repo := filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repo)
	target := filepath.Join(home, ".codex", "worktrees", "reviewed")
	sessions := filepath.Join(home, ".codex", "sessions")
	tool, source := types.ToolCodex, ".codex"
	if orca {
		target = filepath.Join(home, "orca", "workspaces", "project", "reviewed")
		sessions = filepath.Join(testutil.OrcaCodexHome(t, home), "sessions")
		tool, source = types.ToolUnknown, "orca"
	}
	if err := os.MkdirAll(sessions, 0755); err != nil {
		t.Fatal(err)
	}
	var members []string
	for _, name := range []string{"first", "second"} {
		path := filepath.Join(target, name)
		runGitFixture(t, repo, "worktree", "add", "-b", name, path, "HEAD")
		members = append(members, path)
		timestamp := old
		if name == "first" {
			timestamp = old.Add(10 * 24 * time.Hour)
		}
		writeGuidedActivitySession(t, filepath.Join(sessions, name+".jsonl"), path, timestamp)
	}
	item := executorWorktreeItem(target, 900)
	item.Tool, item.Source, item.ModTime = tool, source, old
	return sessions, []types.DebrisInfo{item}, members
}

func writeGuidedActivitySession(t *testing.T, path, cwd string, timestamp time.Time) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"timestamp": timestamp.Format(time.RFC3339Nano), "type": "session_meta", "payload": map[string]any{"session_id": "test", "cwd": cwd}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
}

func guidedActivityState(t *testing.T, items []types.DebrisInfo) guidedCleanState {
	t.Helper()
	state, err := buildGuidedCleanState(context.Background(), &types.ScanResult{Worktrees: items}, scanSource{}, 3*24*time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Rows) != 1 {
		t.Fatalf("rows = %+v", state.Rows)
	}
	return state
}

func TestGuidedReviewRefreshesFreshActivityCache(t *testing.T) {
	sessions, items, members := guidedActivityFixture(t, false)
	first := codexactivity.Load(context.Background())
	if !first.Available {
		t.Fatal(first.Err)
	}
	writeGuidedActivitySession(t, filepath.Join(sessions, "new.jsonl"), members[1], time.Now())
	state := guidedActivityState(t, items)
	if state.Rows[0].Policy != worktree.DecisionLocked || !slices.Contains(state.Rows[0].ReasonCodes, worktree.DecisionReasonRecentActivity) {
		t.Fatalf("fresh cache hid recent session: %+v", state.Rows[0])
	}
}

func TestGuidedReviewUsesSessionFileActivity(t *testing.T) {
	for _, modified := range []bool{false, true} {
		name := "old untouched session"
		if modified {
			name = "old start recent mtime"
		}
		t.Run(name, func(t *testing.T) {
			sessions, items, members := guidedActivityFixture(t, false)
			if err := os.Remove(filepath.Join(sessions, "second.jsonl")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(sessions, "first.jsonl")
			writeGuidedActivitySession(t, path, members[0], time.Now().Add(-10*24*time.Hour))
			if modified {
				now := time.Now()
				if err := os.Chtimes(path, now, now); err != nil {
					t.Fatal(err)
				}
			}
			state := guidedActivityState(t, items)
			row := state.Rows[0]
			if modified {
				if row.Policy != worktree.DecisionLocked || !slices.Contains(row.ReasonCodes, worktree.DecisionReasonRecentActivity) {
					t.Fatalf("recent session-file modification lost protection: %+v", row)
				}
			} else if row.Policy != worktree.DecisionReviewable {
				t.Fatalf("old untouched session falsely locked: %+v", row)
			}
		})
	}
}

func TestGuidedActivityMutationBarrier(t *testing.T) {
	for _, orca := range []bool{false, true} {
		name := "native"
		if orca {
			name = "orca"
		}
		t.Run(name, func(t *testing.T) {
			if orca && runtime.GOOS != "darwin" {
				t.Skip("Orca home is macOS-only")
			}
			changes := []string{"recent session", "newer old session", "appended session", "appended old session", "unavailable store", "unchanged records"}
			if orca {
				changes = append(changes, "other home unavailable")
			}
			for _, change := range changes {
				t.Run(change, func(t *testing.T) {
					sessions, items, members := guidedActivityFixture(t, orca)
					if change == "appended session" || change == "appended old session" {
						// The reviewed owner has only one old, untouched session.
						if err := os.Remove(filepath.Join(sessions, "second.jsonl")); err != nil {
							t.Fatal(err)
						}
					}
					otherStore := filepath.Join(t.TempDir(), "extra-home", "sessions")
					if change == "other home unavailable" {
						if err := os.MkdirAll(otherStore, 0755); err != nil {
							t.Fatal(err)
						}
						t.Setenv("AIBRIS_CODEX_HOMES", filepath.Dir(otherStore))
					}
					state := guidedActivityState(t, items)
					if state.Rows[0].Policy != worktree.DecisionReviewable {
						t.Fatalf("expected reviewable: %+v", state.Rows[0])
					}
					state.Rows[0].Selected = true
					ctx := context.Background()
					plan, err := unifiedCleanupPlanForClean(ctx, &state, nil, CleanupPlanEvidence{}, types.PruneOptions{})
					if err != nil {
						t.Fatal(err)
					}
					safety := staticOverlapSafetyRuntime(nil, nil)
					selection, err := applyCleanupOverlapSafety(ctx, safety, plan.SelectedPhysicalTargets())
					if err != nil {
						t.Fatal(err)
					}
					prepared := prepareGuidedCleanExecutionWithOptions(ctx, selection, safety, types.PruneOptions{}, &state)
					if len(prepared) != 1 || prepared[0].ActivityReview == nil || prepared[0].PreparationError != nil {
						t.Fatalf("review evidence not prepared: %+v", prepared)
					}
					document, err := cleanjson.BuildPlanFromCmd(&types.ScanResult{Worktrees: items}, scanSource{}, types.PruneOptions{}, &state, plan, nil, cleanAudit{}, true)
					if err != nil {
						t.Fatal(err)
					}
					pending, err := newGuidedCleanExecutionReceipt(scanSource{}, types.PruneOptions{}, &state, plan, cleanAudit{}, items, nil, prepared)
					if err != nil {
						t.Fatal(err)
					}
					switch change {
					case "appended session", "appended old session":
						path := filepath.Join(sessions, "first.jsonl")
						f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
						if err != nil {
							t.Fatal(err)
						}
						_, writeErr := f.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n")
						closeErr := f.Close()
						if err := errors.Join(writeErr, closeErr); err != nil {
							t.Fatal(err)
						}
						now := time.Now()
						if change == "appended old session" {
							// Outside the recent window: only advancement beyond review refuses.
							now = now.Add(-10 * 24 * time.Hour)
						}
						if err := os.Chtimes(path, now, now); err != nil {
							t.Fatal(err)
						}
					case "recent session", "newer old session":
						timestamp := time.Now()
						if change == "newer old session" {
							// Newer for the second member, but older than the unit maximum.
							timestamp = timestamp.Add(-25 * 24 * time.Hour)
							if orca {
								// Orca lookup aggregates the entire workspace for every member.
								timestamp = time.Now().Add(-12 * time.Hour)
							}
						}
						writeGuidedActivitySession(t, filepath.Join(sessions, "new.jsonl"), members[1], timestamp)
					case "unavailable store", "other home unavailable":
						if change == "other home unavailable" {
							sessions = otherStore
						}
						if err := os.Rename(sessions, sessions+"-saved"); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(filepath.Dir(sessions), "missing"), sessions); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
					case "unchanged records":
						// Same record-reuse proof as codexactivity's incremental-refresh test:
						// invalid replacement bytes with unchanged size/mtime must not be parsed.
						for _, name := range []string{"first", "second"} {
							path := filepath.Join(sessions, name+".jsonl")
							info, err := os.Stat(path)
							if err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(path, []byte(strings.Repeat("x", int(info.Size()))), 0644); err != nil {
								t.Fatal(err)
							}
							if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
								t.Fatal(err)
							}
						}
					}
					components := cleanjson.SnapshotComponentsFromCmd(plan, nil, items, nil)
					receipt, executionErr := executeCleanJSONReceipt(ctx, confirminput.NewReader(strings.NewReader("")), document, components, plan, prepared, true, false)
					if change == "unchanged records" {
						if executionErr != nil || receipt.Totals.Removed != 1 {
							t.Fatalf("unchanged evidence refused: %+v, %v", receipt, executionErr)
						}
						if _, err := os.Lstat(items[0].Path); !os.IsNotExist(err) {
							t.Fatalf("owner retained: %v", err)
						}
						return
					}
					if !errors.Is(executionErr, worktree.ErrActivityEvidenceChanged) || receipt.Totals.Failed != 1 || receipt.Totals.FreedBytes != 0 || !slices.Contains(receipt.PhysicalTargets[0].ReasonCodes, "activity_evidence_changed") {
						t.Fatalf("activity change allowed removal or lost refusal: %+v, %v", receipt, executionErr)
					}
					for _, path := range members {
						if _, err := os.Stat(path); err != nil {
							t.Fatalf("member removed: %s: %v", path, err)
						}
					}
					// The human guided receipt projection uses the same typed outcome.
					// Execute the already-refused target again to obtain its domain receipt.
					execution, err := executePreparedCleanTargets(ctx, prepared, quietActiveWorktreeExecutionOptions())
					if err == nil || execution.Units[0].MutationAttempted || execution.Units[0].BlockingReason == "" {
						t.Fatalf("missing barrier outcome: %+v, %v", execution, err)
					}
					output := captureOutput(func() { printWorktreeExecutionReceipts(execution) })
					if !strings.Contains(output, "worktree activity evidence changed") || !strings.Contains(output, "not removed") {
						t.Fatalf("human refusal missing: %s", output)
					}
					guidedReceipt, err := pending.finish(execution, err)
					if err != nil || !slices.Contains(guidedReceipt.PhysicalTargets[0].ReasonCodes, "activity_evidence_changed") {
						t.Fatalf("guided refusal lost: %+v, %v", guidedReceipt, err)
					}
				})
			}
		})
	}
}

func TestGuidedActivityReviewSnapshotSurvivesStateChanges(t *testing.T) {
	sessions, items, members := guidedActivityFixture(t, false)
	state := guidedActivityState(t, items)
	safety := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(t.Context(), safety, items)
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareGuidedCleanExecutionWithOptions(t.Context(), selection, safety, types.PruneOptions{}, &state)
	recent := time.Now()
	// Changes to the original UI state must not rebase the prepared decision.
	for i := range state.Units[0].Members {
		for j := range state.Units[0].Members[i].ActivityEvidence {
			state.Units[0].Members[i].ActivityEvidence[j].Timestamp = recent
		}
	}
	writeGuidedActivitySession(t, filepath.Join(sessions, "new.jsonl"), members[1], recent.Add(-25*24*time.Hour))
	receipt, err := executePreparedCleanTargets(t.Context(), prepared, quietActiveWorktreeExecutionOptions())
	if err == nil || receipt.Units[0].MutationAttempted {
		t.Fatalf("review snapshot rebased: %+v, %v", receipt, err)
	}
}

func TestGuidedActivityMutationBarrierRejectsFreshReflog(t *testing.T) {
	_, items, members := guidedActivityFixture(t, false)
	state := guidedActivityState(t, items)
	if state.Rows[0].Policy != worktree.DecisionReviewable {
		t.Fatalf("expected old activity to be reviewable: %+v", state.Rows[0])
	}

	now := time.Now().Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", now)
	t.Setenv("GIT_COMMITTER_DATE", now)
	runGitFixture(t, members[1], "commit", "--allow-empty", "-m", "activity after review")

	// Prepare Git evidence after the commit so HEAD drift cannot mask the live
	// activity check. The attached activity evidence still comes from review.
	state.Rows[0].Selected = true
	plan, err := unifiedCleanupPlanForClean(t.Context(), &state, nil, CleanupPlanEvidence{}, types.PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	safety := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(t.Context(), safety, plan.SelectedPhysicalTargets())
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareGuidedCleanExecutionWithOptions(t.Context(), selection, safety, types.PruneOptions{}, &state)
	if len(prepared) != 1 || prepared[0].ActivityReview == nil || prepared[0].PreparationError != nil {
		t.Fatalf("review evidence not prepared: %+v", prepared)
	}
	execution, err := executePreparedCleanTargets(t.Context(), prepared, quietActiveWorktreeExecutionOptions())
	if !errors.Is(err, worktree.ErrActivityEvidenceChanged) || len(execution.Units) != 1 {
		t.Fatalf("fresh reflog did not refuse execution: %+v, %v", execution, err)
	}
	unit := execution.Units[0]
	if unit.State != cleanExecutionFailed || unit.MutationAttempted || unit.PhysicalRemoved || unit.FreedBytes != 0 || !strings.Contains(unit.BlockingReason, "activity within recent safety window") {
		t.Fatalf("recent-activity refusal lost: %+v", unit)
	}
	reasons := cleanjson.CleanJSONReceiptStateReasons(string(unit.State), unit.PhysicalRemoved, unit.FreedBytes, false, unit.FailureCause, func(error) bool { return false })
	if !slices.Contains(reasons, "activity_evidence_changed") {
		t.Fatalf("activity refusal code lost: %v", reasons)
	}
	for _, path := range append([]string{items[0].Path}, members...) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("reviewed path removed: %s: %v", path, err)
		}
	}
}

func TestGuidedActivityReviewPreservesUnrelatedFailureMapping(t *testing.T) {
	_, items, _ := guidedActivityFixture(t, false)
	state := guidedActivityState(t, items)
	state.Rows[0].Selected = true
	plan, err := unifiedCleanupPlanForClean(t.Context(), &state, nil, CleanupPlanEvidence{}, types.PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	safety := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(t.Context(), safety, plan.SelectedPhysicalTargets())
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareGuidedCleanExecutionWithOptions(t.Context(), selection, safety, types.PruneOptions{}, &state)
	if len(prepared) != 1 || prepared[0].ActivityReview == nil || prepared[0].PreparationError != nil {
		t.Fatalf("review evidence not prepared: %+v", prepared)
	}
	opts := quietActiveWorktreeExecutionOptions()
	opts.Getwd = func() (string, error) { return "", os.ErrPermission }
	execution, err := executePreparedCleanTargets(t.Context(), prepared, opts)
	if !errors.Is(err, os.ErrPermission) || len(execution.Units) != 1 {
		t.Fatalf("unrelated failure lost: %+v, %v", execution, err)
	}
	unit := execution.Units[0]
	if unit.State != cleanExecutionFailed || unit.MutationAttempted || unit.FailureCause != nil {
		t.Fatalf("activity review changed unrelated failure: %+v", unit)
	}
	reasons := cleanjson.CleanJSONReceiptStateReasons(string(unit.State), unit.PhysicalRemoved, unit.FreedBytes, false, unit.FailureCause, func(error) bool { return false })
	if !slices.Equal(reasons, []string{"execution_failed"}) {
		t.Fatalf("unrelated failure mapping changed: %v", reasons)
	}
}

func TestGuidedActivityPreparationRequiresReviewIdentity(t *testing.T) {
	for _, change := range []string{"missing unit", "duplicate unit", "different member", "missing inventory", "wrong activity tool"} {
		t.Run(change, func(t *testing.T) {
			_, items, _ := guidedActivityFixture(t, false)
			state := guidedActivityState(t, items)
			switch change {
			case "missing unit":
				state.Units = nil
			case "duplicate unit":
				state.Units = append(state.Units, state.Units[0])
			case "different member":
				state.Units[0].Members[0].WorktreePath += "-different"
			case "missing inventory":
				state.Inventory = nil
			case "wrong activity tool":
				state.Inventory[0].Tool = types.ToolUnknown
			}
			safety := staticOverlapSafetyRuntime(nil, nil)
			selection, err := applyCleanupOverlapSafety(t.Context(), safety, items)
			if err != nil {
				t.Fatal(err)
			}
			prepared := prepareGuidedCleanExecutionWithOptions(t.Context(), selection, safety, types.PruneOptions{}, &state)
			execution, err := executePreparedCleanTargets(t.Context(), prepared, quietActiveWorktreeExecutionOptions())
			if err == nil || len(execution.Units) != 1 || execution.Units[0].MutationAttempted {
				t.Fatalf("review identity did not refuse: %+v, %v", execution, err)
			}
			unit := execution.Units[0]
			reasons := cleanjson.CleanJSONReceiptStateReasons(string(unit.State), unit.PhysicalRemoved, unit.FreedBytes, false, unit.FailureCause, func(error) bool { return false })
			if !slices.Contains(reasons, "activity_evidence_changed") {
				t.Fatalf("identity refusal lost: %v", reasons)
			}
			if _, err := os.Stat(items[0].Path); err != nil {
				t.Fatalf("owner removed: %v", err)
			}
		})
	}
}

func TestGuidedActivityBarrierAfterMemberPreservesPartialReceipt(t *testing.T) {
	sessions, items, members := guidedActivityFixture(t, false)
	state := guidedActivityState(t, items)
	state.Rows[0].Selected = true
	ctx := t.Context()
	plan, err := unifiedCleanupPlanForClean(ctx, &state, nil, CleanupPlanEvidence{}, types.PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	safety := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(ctx, safety, plan.SelectedPhysicalTargets())
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareGuidedCleanExecutionWithOptions(ctx, selection, safety, types.PruneOptions{}, &state)
	pending, err := newGuidedCleanExecutionReceipt(scanSource{}, types.PruneOptions{}, &state, plan, cleanAudit{}, items, nil, prepared)
	if err != nil {
		t.Fatal(err)
	}
	opts := quietActiveWorktreeExecutionOptions()
	opts.RemoveWorktree = func(ctx context.Context, repo, path string) error {
		if err := worktree.RemoveGitWorktree(ctx, repo, path); err != nil {
			return err
		}
		writeGuidedActivitySession(t, filepath.Join(sessions, "new.jsonl"), members[1], time.Now())
		return nil
	}
	execution, executionErr := executePreparedCleanTargets(ctx, prepared, opts)
	if executionErr == nil || execution.Units[0].State != cleanExecutionPartial || !execution.Units[0].Members[0].Removed || execution.Units[0].Members[1].Removed {
		t.Fatalf("late barrier lost partial outcome: %+v, %v", execution, executionErr)
	}
	if _, err := os.Stat(members[1]); err != nil {
		t.Fatalf("second member removed: %v", err)
	}
	receipt, err := pending.finish(execution, executionErr)
	if err != nil || receipt.Totals.Partial != 1 || !slices.Contains(receipt.PhysicalTargets[0].ReasonCodes, "activity_evidence_changed") || !slices.Contains(receipt.PhysicalTargets[0].ReasonCodes, "partial_failure") {
		t.Fatalf("partial activity refusal lost: %+v, %v", receipt, err)
	}
}
