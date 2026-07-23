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
	Activity   codexActivityIndex
	Policy     CleanupPolicy
	Rows       []guidedCleanRow
	Units      []WorktreeCleanupUnit
	CanReplan  bool
}

type guidedCleanRunResult struct {
	PreviewTargets []types.DebrisInfo
	Aborted        bool
	HadSelection   bool
}

func buildGuidedCleanState(ctx context.Context, result *types.ScanResult, source scanSource, minIdleAge time.Duration, reason string) (guidedCleanState, error) {
	items := activeCodexWorktrees(result.Worktrees)
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
	return newGuidedCleanStateFromCleanupPlan(source, reason, activity, policy, units, items, plan), nil
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

func guidedCleanupUnitItem(unit WorktreeCleanupUnit, items []types.DebrisInfo) types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		path, ok := cleanTargetPathKey(item.Path)
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
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
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

func runGuidedCodexClean(ctx context.Context, opts types.PruneOptions, state guidedCleanState) (guidedCleanRunResult, error) {
	targets, aborted, err := promptGuidedCleanForFiles(os.Stdin, os.Stdout, state)
	if err != nil || aborted {
		return guidedCleanRunResult{Aborted: aborted}, err
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stdout, "No items selected.")
		return guidedCleanRunResult{}, nil
	}

	printCleanPlan(targets, cleanPlanModeDryRun)
	fmt.Fprintln(os.Stdout, "[DRY-RUN] Preview complete.")
	if opts.DryRun {
		fmt.Fprintln(os.Stdout, "[DRY-RUN] No files were removed.")
		return guidedCleanRunResult{PreviewTargets: targets, HadSelection: true}, nil
	}
	prepared := prepareCleanExecution(ctx, targets)

	if opts.Interactive {
		receipt, err := interactiveClean(ctx, prepared)
		printWorktreeExecutionReceipts(receipt)
		printGuidedCleanupReceipt(len(targets), receipt)
		return guidedCleanRunResult{HadSelection: true}, err
	}
	if !opts.Force {
		if !confirmCleanExecution() {
			return guidedCleanRunResult{Aborted: true, HadSelection: true}, nil
		}
	}
	receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
	printWorktreeExecutionReceipts(receipt)
	printGuidedCleanupReceipt(len(targets), receipt)
	if err != nil {
		return guidedCleanRunResult{HadSelection: true}, err
	}
	return guidedCleanRunResult{HadSelection: true}, nil
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

	fmt.Fprintln(output, "guided codex worktree cleanup")
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
	fmt.Fprintf(output, "\nscan\n  source     %s\n  activity   %s\n", cleanAuditScanSourceLine(state.ScanSource), guidedActivitySourceLine(state.Activity))
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
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Row.Item.Size == rows[j].Row.Item.Size {
			return rows[i].Number < rows[j].Number
		}
		return rows[i].Row.Item.Size > rows[j].Row.Item.Size
	})
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
		fmt.Fprintf(output, "  %s %2d  %8s  %-24s %-18s %-11s %s\n",
			box,
			row.Number,
			cleaner.FormatSize(row.Row.Item.Size),
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

func guidedActivitySourceLine(activity codexActivityIndex) string {
	if !activity.Available {
		return "unavailable"
	}
	if activity.Source == codexActivitySourceCache {
		return fmt.Sprintf("cached, %s old", shortDurationString(activity.Age))
	}
	return "indexed"
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
