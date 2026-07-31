package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

const guidedProtectedDisplayLimit = 20

type guidedCleanPolicy = DecisionClass

const (
	guidedCleanPolicyRecommended guidedCleanPolicy = DecisionRecommended
	guidedCleanPolicyReviewable  guidedCleanPolicy = DecisionReviewable
	guidedCleanPolicyLocked      guidedCleanPolicy = DecisionLocked
)

type guidedCleanPromptMode string

const (
	guidedCleanPromptText guidedCleanPromptMode = "text"
	guidedCleanPromptTTY  guidedCleanPromptMode = "tty checklist"
)

type guidedCleanRow struct {
	Number            int
	Key               string
	Row               guidedCodexWorktreeRow
	Policy            guidedCleanPolicy
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

type guidedCleanRunResult struct {
	PreviewTargets    []types.DebrisInfo
	Components        []cleanupOverlapComponent
	SafetyProtections map[string]cleanAuditReason
	Aborted           bool
	HadSelection      bool
}

func buildGuidedCleanState(
	ctx context.Context,
	result *types.ScanResult,
	source scanSource,
	categories []types.Category,
	tools []types.Tool,
	minIdleAge time.Duration,
	reason string,
) (guidedCleanState, error) {
	items := guidedActiveWorktrees(result.Worktrees, categories, tools)
	units, err := buildWorktreeCleanupUnits(ctx, items)
	if err != nil {
		return guidedCleanState{}, err
	}
	activity := loadCodexActivityIndex(ctx)
	if err := enrichWorktreeCleanupActivity(ctx, units, items, worktreeActivityOptions{index: &activity}); err != nil {
		return guidedCleanState{}, err
	}
	cwd, _ := os.Getwd()
	policy := DefaultCleanupPolicy(time.Now())
	policy.CurrentWorkingDirectory = cwd
	policy.MinIdleAge = minIdleAge
	policy = fillCleanupPolicy(policy)
	plan := PlanWorktreeCleanup(units, policy)
	state := newGuidedCleanStateFromCleanupPlan(source, reason, activity, policy, units, items, plan)
	state.Inventory = append([]types.DebrisInfo(nil), result.Worktrees...)
	return state, nil
}

func newGuidedCleanStateFromCleanupPlan(source scanSource, reason string, activity codexActivityIndex, policy CleanupPolicy, units []WorktreeCleanupUnit, items []types.DebrisInfo, plan CleanupPlan) guidedCleanState {
	rows := make([]guidedCleanRow, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		row := guidedCleanRow{
			Key: cleanupUnitStableKey(decision.Unit),
			Row: guidedCodexWorktreeRow{
				Item:   guidedCleanupUnitItem(decision.Unit, items),
				Reason: guidedCleanupDecisionReason(decision),
			},
			Policy: decision.Class,
		}
		row.Selected = row.Policy == guidedCleanPolicyRecommended
		rows = append(rows, row)
	}
	sortGuidedCleanRows(rows)
	for i := range rows {
		rows[i].Number = i + 1
	}
	return guidedCleanState{
		ScanSource: source,
		Reason:     reason,
		Activity:   activity,
		Policy:     fillCleanupPolicy(policy),
		Rows:       rows,
		Units:      units,
		CanReplan:  true,
	}
}

func sortGuidedCleanRows(rows []guidedCleanRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		leftRecommended := rows[i].Policy == guidedCleanPolicyRecommended
		rightRecommended := rows[j].Policy == guidedCleanPolicyRecommended
		if leftRecommended != rightRecommended {
			return leftRecommended
		}
		if rows[i].Row.Item.Size != rows[j].Row.Item.Size {
			return rows[i].Row.Item.Size > rows[j].Row.Item.Size
		}
		return rows[i].Key < rows[j].Key
	})
}

func guidedCleanupUnitItem(unit WorktreeCleanupUnit, items []types.DebrisInfo) types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		path, ok := cleanTargetPathKey(item.Path)
		if ok && path == unit.TargetPath {
			candidates = append(candidates, item)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return guidedWorktreeItemLess(candidates[i], candidates[j])
	})
	item := types.DebrisInfo{
		Tool:     types.ToolUnknown,
		Category: types.CategoryWorktree,
		Status:   types.WorktreeActive,
		Source:   unit.Source,
	}
	if len(candidates) > 0 {
		item = candidates[0]
	}
	item.Path = unit.TargetPath
	item.Size = unit.Size
	if !unit.LastActivity.IsZero() {
		item.ModTime = unit.LastActivity
	}
	return item
}

func guidedWorktreeItemLess(left, right types.DebrisInfo) bool {
	if left.Tool != right.Tool {
		return left.Tool < right.Tool
	}
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.Project != right.Project {
		return left.Project < right.Project
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	return cleanTargetStableKey(left) < cleanTargetStableKey(right)
}

func guidedActiveWorktrees(
	items []types.DebrisInfo,
	categories []types.Category,
	tools []types.Tool,
) []types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		if !isGuidedActiveWorktree(item) ||
			!guidedCategorySelected(categories, item.Category) ||
			!guidedToolSelected(tools, item.Tool) {
			continue
		}
		candidates = append(candidates, item)
	}
	return candidates
}

func isGuidedActiveWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Status == types.WorktreeActive
}

func guidedCategorySelected(categories []types.Category, category types.Category) bool {
	if len(categories) == 0 {
		return true
	}
	for _, selected := range categories {
		if selected == category {
			return true
		}
	}
	return false
}

func guidedToolSelected(tools []types.Tool, tool types.Tool) bool {
	if len(tools) == 0 {
		return true
	}
	for _, selected := range tools {
		if selected == tool {
			return true
		}
	}
	return false
}

func guidedCleanupDecisionReason(decision WorktreeCleanupDecision) string {
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

func runGuidedWorktreeClean(
	ctx context.Context,
	opts types.PruneOptions,
	state guidedCleanState,
	overlapSafety cleanupOverlapSafetyRuntime,
) (guidedCleanRunResult, error) {
	targets, aborted, err := promptGuidedCleanForFiles(os.Stdin, os.Stdout, state)
	if err != nil || aborted {
		return guidedCleanRunResult{Aborted: aborted}, err
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stdout, "No items selected.")
		return guidedCleanRunResult{}, nil
	}
	logicalInputs := cleanupOverlapLogicalInputsForAudit(state.Inventory, opts, nil)
	logicalInputs = applyGuidedPolicyReasons(logicalInputs, state)
	overlapSelection, err := applyCleanupOverlapSafetyWithRows(
		ctx,
		overlapSafety,
		targets,
		logicalInputs,
	)
	if err != nil {
		return guidedCleanRunResult{}, fmt.Errorf("preparing guided overlap safety: %w", err)
	}
	printOverlapSafetyRefusals(overlapSelection)
	targets = overlapSelection.Targets
	if len(targets) == 0 {
		fmt.Fprintln(os.Stdout, "No selected items passed overlap safety.")
		return guidedCleanRunResult{
			Components:        overlapSelection.Components,
			SafetyProtections: overlapSelection.Protections,
			HadSelection:      true,
		}, nil
	}

	printCleanPlanWithComponents(targets, overlapSelection.Components, cleanPlanModeDryRun)
	fmt.Fprintln(os.Stdout, "[DRY-RUN] Preview complete.")
	if opts.DryRun {
		fmt.Fprintln(os.Stdout, "[DRY-RUN] No files were removed.")
		return guidedCleanRunResult{
			PreviewTargets:    targets,
			Components:        overlapSelection.Components,
			SafetyProtections: overlapSelection.Protections,
			HadSelection:      true,
		}, nil
	}
	prepared := prepareCleanExecutionWithOptions(ctx, overlapSelection, overlapSafety, opts)

	if opts.Interactive {
		receipt, err := interactiveClean(ctx, prepared)
		printWorktreeExecutionReceipts(receipt)
		printGuidedCleanupReceipt(len(targets), receipt)
		return guidedCleanRunResult{
			Components:        overlapSelection.Components,
			SafetyProtections: overlapSelection.Protections,
			HadSelection:      true,
		}, err
	}
	if !opts.Force {
		if !confirmCleanExecution() {
			return guidedCleanRunResult{
				Components:        overlapSelection.Components,
				SafetyProtections: overlapSelection.Protections,
				Aborted:           true,
				HadSelection:      true,
			}, nil
		}
	}
	receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
	printWorktreeExecutionReceipts(receipt)
	printGuidedCleanupReceipt(len(targets), receipt)
	if err != nil {
		return guidedCleanRunResult{
			Components:        overlapSelection.Components,
			SafetyProtections: overlapSelection.Protections,
			HadSelection:      true,
		}, err
	}
	return guidedCleanRunResult{
		Components:        overlapSelection.Components,
		SafetyProtections: overlapSelection.Protections,
		HadSelection:      true,
	}, nil
}

func applyGuidedPolicyReasons(
	inputs []cleanupOverlapLogicalInput,
	state guidedCleanState,
) []cleanupOverlapLogicalInput {
	reasonsByPath := make(map[string]string, len(state.Rows))
	for _, row := range state.Rows {
		path, ok := cleanTargetPathKey(row.Row.Item.Path)
		if ok {
			reasonsByPath[path] = row.Row.Reason
		}
	}
	for i := range inputs {
		if !isGuidedActiveWorktree(inputs[i].Item) {
			continue
		}
		path, ok := cleanTargetPathKey(inputs[i].Item.Path)
		if !ok {
			continue
		}
		if reason := reasonsByPath[path]; reason != "" {
			inputs[i].PolicyReason = reason
		}
	}
	return inputs
}

func promptGuidedCleanForFiles(input *os.File, output *os.File, state guidedCleanState) ([]types.DebrisInfo, bool, error) {
	if isTerminal(input) && isTerminal(output) {
		return promptGuidedCleanWithMode(input, output, state, guidedCleanPromptTTY)
	}
	return promptGuidedClean(input, output, state)
}

func promptGuidedClean(input io.Reader, output io.Writer, state guidedCleanState) ([]types.DebrisInfo, bool, error) {
	return promptGuidedCleanWithMode(input, output, state, guidedCleanPromptText)
}

func promptGuidedCleanWithMode(input io.Reader, output io.Writer, state guidedCleanState, mode guidedCleanPromptMode) ([]types.DebrisInfo, bool, error) {
	scanner := bufio.NewScanner(input)
	status := ""
	for {
		renderGuidedClean(output, state, status, mode)
		status = ""
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, false, err
			}
			return selectedGuidedCleanTargets(state), false, nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			return selectedGuidedCleanTargets(state), false, nil
		}
		if strings.EqualFold(line, "q") {
			fmt.Fprintln(output, "Aborted.")
			return nil, true, nil
		}
		if next, message, ok := applyGuidedCleanCommand(state, line); ok {
			state = next
			status = message
			continue
		}
		for _, field := range strings.FieldsFunc(line, guidedToggleSeparator) {
			if field == "" {
				continue
			}
			n, err := strconv.Atoi(field)
			if err != nil {
				continue
			}
			if !toggleGuidedCleanRow(&state, n) {
				status = fmt.Sprintf("row %d is locked and cannot be selected", n)
			}
		}
	}
}

func guidedToggleSeparator(r rune) bool {
	return r == ',' || r == ' ' || r == '\t'
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

func selectedGuidedCleanTargets(state guidedCleanState) []types.DebrisInfo {
	var targets []types.DebrisInfo
	for _, row := range state.Rows {
		if row.Selected {
			targets = append(targets, row.Row.Item)
		}
	}
	return normalizeCleanTargets(targets)
}

func renderGuidedClean(output io.Writer, state guidedCleanState, status string, mode guidedCleanPromptMode) {
	policy := fillCleanupPolicy(state.Policy)
	selectedCount, selectedSize := guidedSelectionTotals(state)
	protectedRows := guidedProtectedRows(state)
	protectedCount, protectedSize := guidedProtectedTotals(state)
	projectedFreed := guidedProjectedFreedSize(state)

	fmt.Fprintln(output, "guided worktree cleanup")
	if mode == guidedCleanPromptTTY {
		fmt.Fprintf(output, "  mode       %s\n", mode)
	}
	if state.Reason != "" {
		fmt.Fprintf(output, "  reason     %s\n", state.Reason)
	}
	fmt.Fprintf(output, "  policy     idle>%s, recent<%s locked, keep=%d/repo, min-size=%s\n",
		guidedAgeString(policy.MinIdleAge),
		guidedAgeString(policy.RecentActivityWindow),
		policy.KeepPerRepository,
		cleaner.FormatSize(policy.MinSize))
	if status != "" {
		fmt.Fprintf(output, "  status     %s\n", status)
	}
	fmt.Fprintf(output, "\nscan\n  source     %s\n  activity   %s\n", cleanAuditScanSourceLine(state.ScanSource), guidedActivitySourceLine(state.Activity, state.Rows))
	fmt.Fprintf(output, "\nsummary\n  selected   %d %s   %s\n  projected  %s\n  protected  %d %s   %s\n",
		selectedCount, itemNoun(selectedCount), cleaner.FormatSize(selectedSize),
		cleaner.FormatSize(projectedFreed),
		protectedCount, itemNoun(protectedCount), cleaner.FormatSize(protectedSize))

	fmt.Fprintln(output, "\nselected for cleanup")
	renderGuidedRows(output, state.Rows, true, 0)
	fmt.Fprintln(output, "\nprotected")
	renderGuidedRows(output, protectedRows, false, guidedProtectedDisplayLimit)
	if len(protectedRows) > guidedProtectedDisplayLimit {
		remaining := protectedRows[guidedProtectedDisplayLimit:]
		var size int64
		for _, row := range remaining {
			size += row.Row.Item.Size
		}
		fmt.Fprintf(output, "  ... %d more protected   %s\n", len(remaining), cleaner.FormatSize(size))
	}
	fmt.Fprint(output, "\nEnter numbers to toggle, age <duration> (minimum idle), +/- age, Enter to preview, q to abort: ")
}

func guidedSelectionTotals(state guidedCleanState) (int, int64) {
	var count int
	var size int64
	for _, row := range state.Rows {
		if row.Selected {
			count++
			size += row.Row.Item.Size
		}
	}
	return count, size
}

func guidedProtectedTotals(state guidedCleanState) (int, int64) {
	var count int
	var size int64
	for _, row := range state.Rows {
		if !row.Selected {
			count++
			size += row.Row.Item.Size
		}
	}
	return count, size
}

func guidedProjectedFreedSize(state guidedCleanState) int64 {
	var size int64
	for _, item := range selectedGuidedCleanTargets(state) {
		size += item.Size
	}
	return size
}

func guidedProtectedRows(state guidedCleanState) []guidedCleanRow {
	var rows []guidedCleanRow
	for _, row := range state.Rows {
		if !row.Selected {
			rows = append(rows, row)
		}
	}
	return rows
}

func renderGuidedRows(output io.Writer, rows []guidedCleanRow, selected bool, limit int) {
	shown := 0
	for _, row := range rows {
		if row.Selected != selected {
			continue
		}
		if limit > 0 && shown >= limit {
			return
		}
		box := "[ ]"
		if row.Selected {
			box = "[x]"
		} else if row.Policy == guidedCleanPolicyLocked {
			box = "[!]"
		}
		fmt.Fprintf(output, "  %s %2d  %8s  %-10s %-24s %-18s %-11s %s\n",
			box,
			row.Number,
			cleaner.FormatSize(row.Row.Item.Size),
			row.Row.Item.Tool,
			itemName(row.Row.Item),
			itemAgeAndStatus(row.Row.Item),
			row.Policy,
			row.Row.Reason)
		shown++
	}
	if shown == 0 {
		fmt.Fprintln(output, "  -")
	}
}

func guidedActivitySourceLine(activity codexActivityIndex, rows []guidedCleanRow) string {
	status := "unavailable"
	if !activity.Available {
		status = "unavailable"
	} else if activity.Source == codexActivitySourceCache {
		status = fmt.Sprintf("cached, %s old", shortDurationString(activity.Age))
	} else {
		status = "indexed"
	}
	for _, row := range rows {
		if row.Row.Item.Tool != types.ToolCodex {
			return "codex " + status + "; unregistered tools noted per row"
		}
	}
	return "codex " + status
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

func replanGuidedCleanAge(state guidedCleanState, age time.Duration) (guidedCleanState, string) {
	if !state.CanReplan {
		return state, "age threshold cannot be changed in this context"
	}
	overrides := guidedCleanSelectionOverrides(state)
	next := state
	next.Rows = append([]guidedCleanRow(nil), state.Rows...)
	next.Policy = fillCleanupPolicy(state.Policy)
	next.Policy.MinIdleAge = age
	decisions := make(map[string]WorktreeCleanupDecision, len(state.Units))
	for _, decision := range PlanWorktreeCleanup(state.Units, next.Policy).Decisions {
		decisions[cleanupUnitStableKey(decision.Unit)] = decision
	}
	for i := range next.Rows {
		decision, ok := decisions[next.Rows[i].Key]
		if !ok {
			continue
		}
		next.Rows[i].Policy = decision.Class
		next.Rows[i].Row.Reason = guidedCleanupDecisionReason(decision)
		next.Rows[i].Selected = next.Rows[i].Policy == guidedCleanPolicyRecommended
	}
	applyGuidedCleanSelectionOverrides(&next, overrides)
	// Reapply the display contract after policy changes while leaving Number
	// attached to the cleanup-unit identity for stable selection commands.
	sortGuidedCleanRows(next.Rows)
	return next, fmt.Sprintf("minimum idle age set to %s", guidedAgeString(age))
}

func guidedCleanSelectionOverrides(state guidedCleanState) map[string]bool {
	overrides := make(map[string]bool)
	for _, row := range state.Rows {
		if row.SelectionOverride != nil {
			overrides[row.Key] = *row.SelectionOverride
			continue
		}
		defaultSelected := row.Policy == guidedCleanPolicyRecommended
		if row.Selected != defaultSelected {
			overrides[row.Key] = row.Selected
		}
	}
	return overrides
}

func applyGuidedCleanSelectionOverrides(state *guidedCleanState, overrides map[string]bool) {
	for i := range state.Rows {
		selected, ok := overrides[state.Rows[i].Key]
		if state.Rows[i].Policy == guidedCleanPolicyLocked {
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

func guidedAgeString(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}
