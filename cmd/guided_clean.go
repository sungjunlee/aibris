package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type guidedCleanPolicy = DecisionClass

const (
	guidedCleanPolicyRecommended guidedCleanPolicy = DecisionRecommended
	guidedCleanPolicyReviewable  guidedCleanPolicy = DecisionReviewable
	guidedCleanPolicyLocked      guidedCleanPolicy = DecisionLocked
)

type guidedCleanRow struct {
	Number            int
	Key               string
	Row               guidedCodexWorktreeRow
	Policy            guidedCleanPolicy
	ReasonCodes       []DecisionReasonCode
	Selected          bool
	SelectionOverride *bool
}

type guidedCleanState struct {
	ScanSource scanSource
	Reason     string
	Inventory  []types.DebrisInfo
	Activity   codexActivityIndex
	Policy     CleanupPolicy
	Rows       []guidedCleanRow
	Units      []WorktreeCleanupUnit
	CanReplan  bool
}

func buildGuidedCleanState(ctx context.Context, result *types.ScanResult, source scanSource, minIdleAge time.Duration, reason string) (guidedCleanState, error) {
	items := activeWorktrees(result.Worktrees)
	units, err := worktree.BuildWorktreeCleanupUnits(ctx, items)
	if err != nil {
		return guidedCleanState{}, err
	}
	activity := loadCodexActivityIndex(ctx)
	if err := enrichWorktreeCleanupActivity(ctx, units, items, worktreeActivityOptions{index: &activity}); err != nil {
		return guidedCleanState{}, err
	}
	return planGuidedCleanState(ctx, result, source, reason, activity, units, items, minIdleAge)
}

func planGuidedCleanState(ctx context.Context, result *types.ScanResult, source scanSource, reason string, activity codexActivityIndex, units []WorktreeCleanupUnit, items []types.DebrisInfo, minIdleAge time.Duration) (guidedCleanState, error) {
	cwd, _ := os.Getwd()
	policy := DefaultCleanupPolicy(time.Now())
	policy.CurrentWorkingDirectory = cwd
	policy.MinIdleAge = minIdleAge
	policy = fillCleanupPolicy(policy)
	worktree.InspectRecommendedCandidateUniqueness(ctx, units, policy)
	plan := worktree.PlanWorktreeCleanup(units, policy)
	state := newGuidedCleanStateFromCleanupPlan(source, reason, activity, policy, units, items, plan)
	state.Inventory = append([]types.DebrisInfo(nil), result.Worktrees...)
	return state, nil
}

func toggleGuidedCleanRow(state *guidedCleanState, number int) bool {
	for i := range state.Rows {
		if state.Rows[i].Number == number {
			if state.Rows[i].Policy == guidedCleanPolicyLocked {
				return false
			}
			state.Rows[i].Selected = !state.Rows[i].Selected
			selected := state.Rows[i].Selected
			state.Rows[i].SelectionOverride = &selected
			return true
		}
	}
	return true
}

func applyGuidedCleanCommand(state guidedCleanState, line string) (guidedCleanState, string, bool) {
	switch strings.ToLower(line) {
	case "+", "]":
		return adjustGuidedCleanAge(state, 1)
	case "-", "[":
		return adjustGuidedCleanAge(state, -1)
	}
	if strings.HasPrefix(strings.ToLower(line), "age ") {
		value := strings.TrimSpace(line[4:])
		age, err := parseAge(value)
		if err != nil || age <= 0 {
			return state, "invalid age duration", true
		}
		next, message := replanGuidedCleanAge(state, age)
		return next, message, true
	}
	return state, "", false
}

func adjustGuidedCleanAge(state guidedCleanState, direction int) (guidedCleanState, string, bool) {
	current := fillCleanupPolicy(state.Policy).MinIdleAge
	if current <= 0 {
		current = DefaultMinIdleAge
	}
	presets := guidedCleanAgePresets(current)
	index := 0
	for i, preset := range presets {
		if preset == current {
			index = i
			break
		}
		if preset < current {
			index = i + 1
		}
	}
	index += direction
	if index < 0 {
		index = 0
	}
	if index >= len(presets) {
		index = len(presets) - 1
	}
	nextAge := presets[index]
	next, message := replanGuidedCleanAge(state, nextAge)
	return next, message, true
}

func replanGuidedCleanAge(state guidedCleanState, age time.Duration) (guidedCleanState, string) {
	if !state.CanReplan {
		return state, "age threshold cannot be changed in this context"
	}
	overrides := guidedCleanSelectionOverrides(state)
	next := cloneGuidedCleanStateForReplan(state)
	next.Policy = fillCleanupPolicy(state.Policy)
	next.Policy.MinIdleAge = age
	applyReplannedGuidedCleanup(&next)
	applyGuidedCleanSelectionOverrides(&next, overrides)
	return next, fmt.Sprintf("minimum idle age set to %s", guidedAgeString(age))
}
