package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/types"
)

// GuidedCleanRow represents a single worktree in the guided cleanup UI.
type GuidedCleanRow struct {
	Number            int
	Key               string
	Row               GuidedCodexWorktreeRow
	Policy            DecisionClass
	ReasonCodes       []DecisionReasonCode
	Selected          bool
	SelectionOverride *bool
}

// GuidedCleanState holds the complete state for guided cleanup interaction.
type GuidedCleanState struct {
	ScanSource cleaner.ScanSource
	Reason     string
	Inventory  []types.DebrisInfo
	Activity   codexactivity.Index
	Policy     CleanupPolicy
	Rows       []GuidedCleanRow
	Units      []WorktreeCleanupUnit
	CanReplan  bool
}

// GuidedCodexWorktreeRow holds display data for a guided cleanup row.
type GuidedCodexWorktreeRow struct {
	Item   types.DebrisInfo
	Reason string
}

// BuildGuidedCleanStateOptions holds optional dependencies for building guided state.
type BuildGuidedCleanStateOptions struct {
	CurrentWorkingDirectory string
	Activity                *codexactivity.Index
	ProtectMatcher          *exclude.Matcher
}

// BuildGuidedCleanState constructs guided cleanup state from scan results.
func BuildGuidedCleanState(
	ctx context.Context,
	result *types.ScanResult,
	source cleaner.ScanSource,
	minIdleAge time.Duration,
	reason string,
	opts BuildGuidedCleanStateOptions,
) (GuidedCleanState, error) {
	items := filterActiveWorktrees(result.Worktrees)
	units, err := BuildWorktreeCleanupUnits(ctx, items)
	if err != nil {
		return GuidedCleanState{}, err
	}

	activity := codexactivity.Index{}
	if opts.Activity != nil {
		activity = *opts.Activity
	} else {
		activity = codexactivity.Load(ctx)
	}

	activityOpts := ActivityOptions{Index: &activity}
	if err := EnrichActivity(ctx, units, items, activityOpts); err != nil {
		return GuidedCleanState{}, err
	}

	return planGuidedCleanState(ctx, result, source, reason, activity, units, items, minIdleAge, opts)
}

func planGuidedCleanState(
	ctx context.Context,
	result *types.ScanResult,
	source cleaner.ScanSource,
	reason string,
	activity codexactivity.Index,
	units []WorktreeCleanupUnit,
	items []types.DebrisInfo,
	minIdleAge time.Duration,
	opts BuildGuidedCleanStateOptions,
) (GuidedCleanState, error) {
	policy := DefaultCleanupPolicy(time.Now())
	policy.CurrentWorkingDirectory = opts.CurrentWorkingDirectory
	policy.MinIdleAge = minIdleAge
	policy = FillCleanupPolicy(policy)

	InspectRecommendedCandidateUniqueness(ctx, units, policy)
	plan := PlanWorktreeCleanup(units, policy)
	state := newGuidedCleanStateFromCleanupPlan(source, reason, activity, policy, units, items, plan)
	state.Inventory = append([]types.DebrisInfo(nil), result.Worktrees...)
	applyProtectPathToGuidedState(&state, opts.ProtectMatcher)
	return state, nil
}

// ToggleGuidedCleanRow toggles the selection state of a row by number.
// Returns false if the row is locked or not found.
func ToggleGuidedCleanRow(state *GuidedCleanState, number int) bool {
	for i := range state.Rows {
		if state.Rows[i].Number == number {
			if state.Rows[i].Policy == DecisionLocked {
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

// ApplyGuidedCleanCommand processes user commands that modify guided state.
// Supports age adjustment commands: "+", "]" (increase), "-", "[" (decrease), "age <duration>".
// Returns (newState, message, handled).
func ApplyGuidedCleanCommand(
	ctx context.Context,
	state GuidedCleanState,
	line string,
	parseAgeFn func(string) (time.Duration, error),
) (GuidedCleanState, string, bool) {
	switch strings.ToLower(line) {
	case "+", "]":
		return adjustGuidedCleanAge(ctx, state, 1)
	case "-", "[":
		return adjustGuidedCleanAge(ctx, state, -1)
	}
	if strings.HasPrefix(strings.ToLower(line), "age ") {
		value := strings.TrimSpace(line[4:])
		age, err := parseAgeFn(value)
		if err != nil || age <= 0 {
			return state, "invalid age duration", true
		}
		next, message := replanGuidedCleanAge(ctx, state, age)
		return next, message, true
	}
	return state, "", false
}

func adjustGuidedCleanAge(ctx context.Context, state GuidedCleanState, direction int) (GuidedCleanState, string, bool) {
	current := FillCleanupPolicy(state.Policy).MinIdleAge
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
	next, message := replanGuidedCleanAge(ctx, state, nextAge)
	return next, message, true
}

func replanGuidedCleanAge(ctx context.Context, state GuidedCleanState, age time.Duration) (GuidedCleanState, string) {
	if !state.CanReplan {
		return state, "age threshold cannot be changed in this context"
	}
	overrides := guidedCleanSelectionOverrides(state)
	next := cloneGuidedCleanStateForReplan(state)
	next.Policy = FillCleanupPolicy(state.Policy)
	next.Policy.MinIdleAge = age
	applyReplannedGuidedCleanup(ctx, &next)
	applyGuidedCleanSelectionOverrides(&next, overrides)
	return next, fmt.Sprintf("minimum idle age set to %s", GuidedAgeString(age))
}

// NewGuidedCleanStateFromCleanupPlan constructs guided state from a cleanup plan.
// Exposed for testing.
func NewGuidedCleanStateFromCleanupPlan(
	source cleaner.ScanSource,
	reason string,
	activity codexactivity.Index,
	policy CleanupPolicy,
	units []WorktreeCleanupUnit,
	items []types.DebrisInfo,
	plan CleanupPlan,
) GuidedCleanState {
	return newGuidedCleanStateFromCleanupPlan(source, reason, activity, policy, units, items, plan)
}

func newGuidedCleanStateFromCleanupPlan(
	source cleaner.ScanSource,
	reason string,
	activity codexactivity.Index,
	policy CleanupPolicy,
	units []WorktreeCleanupUnit,
	items []types.DebrisInfo,
	plan CleanupPlan,
) GuidedCleanState {
	rows := make([]GuidedCleanRow, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		row := GuidedCleanRow{
			Key: CleanupUnitStableKey(decision.Unit),
			Row: GuidedCodexWorktreeRow{
				Item:   guidedCleanupUnitItem(decision.Unit, items),
				Reason: GuidedCleanupDecisionReason(decision),
			},
			Policy: decision.Class,
		}
		for _, reason := range decision.Reasons {
			row.ReasonCodes = append(row.ReasonCodes, reason.Code)
		}
		row.Selected = row.Policy == DecisionRecommended
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftRecommended := rows[i].Policy == DecisionRecommended
		rightRecommended := rows[j].Policy == DecisionRecommended
		if leftRecommended != rightRecommended {
			return leftRecommended
		}
		if rows[i].Row.Item.Size != rows[j].Row.Item.Size {
			return rows[i].Row.Item.Size > rows[j].Row.Item.Size
		}
		return rows[i].Key < rows[j].Key
	})
	for i := range rows {
		rows[i].Number = i + 1
	}
	return GuidedCleanState{
		ScanSource: source,
		Reason:     reason,
		Activity:   activity,
		Policy:     FillCleanupPolicy(policy),
		Rows:       rows,
		Units:      units,
		CanReplan:  true,
	}
}

// GuidedCleanupUnitItem finds the debris item that matches a cleanup unit.
func GuidedCleanupUnitItem(unit WorktreeCleanupUnit, items []types.DebrisInfo) types.DebrisInfo {
	return guidedCleanupUnitItem(unit, items)
}

func guidedCleanupUnitItem(unit WorktreeCleanupUnit, items []types.DebrisInfo) types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		path, ok := cleaner.TargetPathKey(item.Path)
		if ok && path == unit.TargetPath {
			candidates = append(candidates, item)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Project != candidates[j].Project {
			return candidates[i].Project < candidates[j].Project
		}
		if candidates[i].ID != candidates[j].ID {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Path < candidates[j].Path
	})
	// Fallback identity for a unit no scanner row named. The tool stays
	// unknown rather than assuming Codex now that review admits every tool.
	item := types.DebrisInfo{
		Tool:     types.ToolUnknown,
		Category: types.CategoryWorktree,
		Status:   types.WorktreeActive,
	}
	if len(candidates) > 0 {
		item = candidates[0]
	}
	item.Path = unit.TargetPath
	item.Size = unit.Size
	item.Source = unit.Source
	if !unit.LastActivity.IsZero() {
		item.ModTime = unit.LastActivity
	}
	return item
}

// GuidedCleanupDecisionReason formats a human-readable reason for a cleanup decision.
func GuidedCleanupDecisionReason(decision WorktreeCleanupDecision) string {
	parts := make([]string, 0, len(decision.Reasons)+len(decision.Unit.Members))
	for _, reason := range decision.Reasons {
		value := reason.Description
		if value == "" {
			value = string(reason.Code)
		}
		if reason.WorktreePath != "" && len(decision.Unit.Members) > 1 {
			value = filepath.Base(reason.WorktreePath) + ": " + value
		}
		parts = append(parts, value)
	}
	members := append([]GitWorktreeMember(nil), decision.Unit.Members...)
	sort.Slice(members, func(i, j int) bool {
		return members[i].WorktreePath < members[j].WorktreePath
	})
	for _, member := range members {
		switch member.Upstream.State {
		case GitUpstreamNone:
			parts = append(parts, guidedMemberReason(decision.Unit, member, "no upstream configured"))
		case GitUpstreamGone:
			parts = append(parts, guidedMemberReason(decision.Unit, member, "upstream gone: "+member.Upstream.Ref))
		}
	}
	if len(parts) == 0 {
		return "cleanup policy decision"
	}
	return strings.Join(parts, "; ")
}

func guidedMemberReason(unit WorktreeCleanupUnit, member GitWorktreeMember, reason string) string {
	if len(unit.Members) > 1 {
		return filepath.Base(member.WorktreePath) + ": " + reason
	}
	return reason
}

// ApplyGuidedPolicyReasons enriches overlap inputs with guided policy reasons.
func ApplyGuidedPolicyReasons(
	inputs []cleaner.CleanupOverlapLogicalInput,
	state GuidedCleanState,
) []cleaner.CleanupOverlapLogicalInput {
	reasonsByPath := make(map[string]string, len(state.Rows))
	for _, row := range state.Rows {
		path, ok := cleaner.TargetPathKey(row.Row.Item.Path)
		if ok {
			reasonsByPath[path] = row.Row.Reason
		}
	}
	for i := range inputs {
		if inputs[i].Item.Category != types.CategoryWorktree || inputs[i].Item.Status != types.WorktreeActive {
			continue
		}
		path, ok := cleaner.TargetPathKey(inputs[i].Item.Path)
		if !ok {
			continue
		}
		if reason := reasonsByPath[path]; reason != "" {
			inputs[i].PolicyReason = reason
		}
	}
	return inputs
}

// SelectedGuidedCleanTargets returns the debris items selected for cleanup.
func SelectedGuidedCleanTargets(state GuidedCleanState) []types.DebrisInfo {
	var targets []types.DebrisInfo
	for _, row := range state.Rows {
		if row.Selected {
			targets = append(targets, row.Row.Item)
		}
	}
	return cleaner.NormalizeTargets(targets)
}

func guidedCleanAgePresets(current time.Duration) []time.Duration {
	presets := []time.Duration{
		6 * time.Hour,
		24 * time.Hour,
		3 * 24 * time.Hour,
		7 * 24 * time.Hour,
		14 * 24 * time.Hour,
		30 * 24 * time.Hour,
	}
	found := false
	for _, preset := range presets {
		if preset == current {
			found = true
			break
		}
	}
	if !found {
		presets = append(presets, current)
		sort.Slice(presets, func(i, j int) bool { return presets[i] < presets[j] })
	}
	return presets
}

func cloneGuidedCleanStateForReplan(state GuidedCleanState) GuidedCleanState {
	next := state
	next.Rows = append([]GuidedCleanRow(nil), state.Rows...)
	for i := range next.Rows {
		next.Rows[i].ReasonCodes = append([]DecisionReasonCode(nil), state.Rows[i].ReasonCodes...)
	}
	next.Units = append([]WorktreeCleanupUnit(nil), state.Units...)
	for i := range next.Units {
		next.Units[i].Members = append([]GitWorktreeMember(nil), state.Units[i].Members...)
	}
	return next
}

func applyReplannedGuidedCleanup(ctx context.Context, state *GuidedCleanState) {
	InspectRecommendedCandidateUniqueness(ctx, state.Units, state.Policy)
	decisions := make(map[string]WorktreeCleanupDecision, len(state.Units))
	for _, decision := range PlanWorktreeCleanup(state.Units, state.Policy).Decisions {
		decisions[CleanupUnitStableKey(decision.Unit)] = decision
	}
	for i := range state.Rows {
		applyReplannedGuidedRow(&state.Rows[i], decisions)
	}
}

func applyReplannedGuidedRow(row *GuidedCleanRow, decisions map[string]WorktreeCleanupDecision) {
	decision, ok := decisions[row.Key]
	if !ok {
		return
	}
	row.Policy = decision.Class
	row.Row.Reason = GuidedCleanupDecisionReason(decision)
	row.ReasonCodes = row.ReasonCodes[:0]
	for _, reason := range decision.Reasons {
		row.ReasonCodes = append(row.ReasonCodes, reason.Code)
	}
	row.Selected = row.Policy == DecisionRecommended
}

func guidedCleanSelectionOverrides(state GuidedCleanState) map[string]bool {
	overrides := make(map[string]bool)
	for _, row := range state.Rows {
		if row.SelectionOverride != nil {
			overrides[row.Key] = *row.SelectionOverride
			continue
		}
		defaultSelected := row.Policy == DecisionRecommended
		if row.Selected != defaultSelected {
			overrides[row.Key] = row.Selected
		}
	}
	return overrides
}

func applyGuidedCleanSelectionOverrides(state *GuidedCleanState, overrides map[string]bool) {
	for i := range state.Rows {
		selected, ok := overrides[state.Rows[i].Key]
		if state.Rows[i].Policy == DecisionLocked {
			state.Rows[i].Selected = false
			state.Rows[i].SelectionOverride = nil
			continue
		}
		if !ok {
			continue
		}
		state.Rows[i].Selected = selected
		state.Rows[i].SelectionOverride = &selected
	}
}

// GuidedAgeString formats a duration for display in guided UI.
func GuidedAgeString(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}

// GuidedCodexWorktreeContainsCWD checks if a worktree path contains the current working directory.
func GuidedCodexWorktreeContainsCWD(worktreePath, cwd string) bool {
	if cwd == "" {
		return false
	}
	worktree, ok := cleaner.TargetPathKey(worktreePath)
	if !ok {
		return false
	}
	current, ok := cleaner.TargetPathKey(cwd)
	if !ok {
		return false
	}
	return worktree == current || cleaner.PathContains(worktree, current)
}

func applyProtectPathToGuidedState(state *GuidedCleanState, matcher *exclude.Matcher) {
	if state == nil || matcher == nil {
		return
	}
	for i := range state.Rows {
		if !matcher.ProtectMatch(state.Rows[i].Row.Item.Path) {
			continue
		}
		state.Rows[i].Policy = DecisionLocked
		state.Rows[i].Selected = false
		state.Rows[i].SelectionOverride = nil
		state.Rows[i].Row.Reason = "protected by --protect-path"
		state.Rows[i].ReasonCodes = []DecisionReasonCode{"protect_path"}
	}
}

func filterActiveWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	var active []types.DebrisInfo
	for _, item := range items {
		if item.Category == types.CategoryWorktree && item.Status == types.WorktreeActive {
			active = append(active, item)
		}
	}
	return active
}
