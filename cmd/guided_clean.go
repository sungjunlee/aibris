package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

const guidedProtectedDisplayLimit = 20

type guidedCleanPolicy string

const (
	guidedCleanPolicyRecommended guidedCleanPolicy = "recommended"
	guidedCleanPolicyReviewable  guidedCleanPolicy = "reviewable"
	guidedCleanPolicyLocked      guidedCleanPolicy = "locked"
)

type guidedCleanPromptMode string

const (
	guidedCleanPromptText guidedCleanPromptMode = "text"
	guidedCleanPromptTTY  guidedCleanPromptMode = "tty checklist"
)

type guidedCleanRow struct {
	Number   int
	Key      string
	Row      guidedCodexWorktreeRow
	Policy   guidedCleanPolicy
	Selected bool
}

type guidedCleanState struct {
	ScanSource scanSource
	Reason     string
	Activity   codexActivityIndex
	Age        time.Duration
	Rows       []guidedCleanRow
	PlanInput  guidedCodexWorktreePlanInput
	CanReplan  bool
}

func buildGuidedCleanState(ctx context.Context, result *types.ScanResult, source scanSource, age time.Duration, reason string) guidedCleanState {
	activity := loadCodexActivityIndex(ctx)
	gitSafety := inspectGuidedCodexGitSafety(ctx, result.Worktrees)
	cwd, _ := os.Getwd()
	input := guidedCodexWorktreePlanInput{
		Worktrees:               result.Worktrees,
		Activity:                activity,
		GitSafety:               gitSafety,
		CurrentWorkingDirectory: cwd,
		Age:                     age,
	}
	return newGuidedCleanStateFromPlanInput(source, reason, input)
}

func runGuidedCodexClean(ctx context.Context, opts types.PruneOptions, state guidedCleanState) error {
	targets, aborted, err := promptGuidedCleanForFiles(os.Stdin, os.Stdout, state)
	if err != nil || aborted {
		return err
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stdout, "No items selected.")
		return nil
	}

	printCleanPlan(targets, cleanPlanModeDryRun)
	fmt.Fprintln(os.Stdout, "[DRY-RUN] Preview complete.")
	if opts.DryRun {
		fmt.Fprintln(os.Stdout, "[DRY-RUN] No files were removed.")
		return nil
	}
	prepared := prepareCleanExecution(ctx, targets)

	if opts.Interactive {
		receipt := interactiveClean(ctx, prepared)
		printWorktreeExecutionReceipts(receipt)
		printGuidedCleanupReceipt(len(targets), receipt)
		return nil
	}
	if !opts.Force {
		if !confirmCleanExecution() {
			return nil
		}
	}
	receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
	printWorktreeExecutionReceipts(receipt)
	printGuidedCleanupReceipt(len(targets), receipt)
	if err != nil {
		return err
	}
	return nil
}

func promptGuidedCleanForFiles(input *os.File, output *os.File, state guidedCleanState) ([]types.DebrisInfo, bool, error) {
	if isTerminal(input) && isTerminal(output) {
		return promptGuidedCleanWithMode(input, output, state, guidedCleanPromptTTY)
	}
	return promptGuidedClean(input, output, state)
}

func inspectGuidedCodexGitSafety(ctx context.Context, items []types.DebrisInfo) map[string]worktreeGitSafety {
	safety := make(map[string]worktreeGitSafety)
	for _, item := range activeCodexWorktrees(items) {
		safety[item.Path] = inspectActiveWorktreeCleanupSafety(ctx, item.Path)
		if path, ok := cleanTargetPathKey(item.Path); ok {
			safety[path] = safety[item.Path]
		}
	}
	return safety
}

func newGuidedCleanStateFromPlanInput(source scanSource, reason string, input guidedCodexWorktreePlanInput) guidedCleanState {
	input = fillGuidedCodexWorktreePlanInput(input)
	plan := buildGuidedCodexWorktreePlan(input)
	state := newGuidedCleanState(source, reason, input.Activity, plan)
	state.Age = input.Age
	state.PlanInput = input
	state.CanReplan = true
	return state
}

func newGuidedCleanState(source scanSource, reason string, activity codexActivityIndex, plan guidedCodexWorktreePlan) guidedCleanState {
	rows := make([]guidedCleanRow, 0, len(plan.Selected)+len(plan.Protected))
	number := 1
	for _, row := range plan.Selected {
		rows = append(rows, guidedCleanRow{
			Number:   number,
			Key:      guidedCodexStableItemKey(row.Item),
			Row:      row,
			Policy:   guidedCleanPolicyRecommended,
			Selected: true,
		})
		number++
	}
	for _, row := range plan.Protected {
		rows = append(rows, guidedCleanRow{
			Number: number,
			Key:    guidedCodexStableItemKey(row.Item),
			Row:    row,
			Policy: guidedCleanPolicyForProtectedReason(row.Reason),
		})
		number++
	}
	return guidedCleanState{ScanSource: source, Reason: reason, Activity: activity, Rows: rows}
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
	if state.Age > 0 {
		fmt.Fprintf(output, "  age        > %s\n", guidedAgeString(state.Age))
	}
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
	fmt.Fprint(output, "\nEnter numbers to toggle, age <duration>, +/- age, Enter to preview, q to abort: ")
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
		fmt.Fprintf(output, "  %s %2d  %8s  %-24s %-18s %s\n",
			box,
			row.Number,
			cleaner.FormatSize(row.Row.Item.Size),
			itemName(row.Row.Item),
			itemAgeAndStatus(row.Row.Item),
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

func guidedCleanPolicyForProtectedReason(reason string) guidedCleanPolicy {
	switch reason {
	case guidedCodexProtectionNewestProjectWorktree,
		guidedCodexProtectionLatestSessionToday,
		guidedCodexProtectionRecentActivity,
		guidedCodexProtectionBelowSizeThreshold,
		guidedCodexProtectionYoungerThanGuideAge,
		guidedCodexProtectionNoLowRiskSignal:
		return guidedCleanPolicyReviewable
	default:
		return guidedCleanPolicyLocked
	}
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
	current := state.Age
	if current <= 0 {
		current = guidedCodexDefaultAge
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
	input := state.PlanInput
	input.Age = age
	next := newGuidedCleanStateFromPlanInput(state.ScanSource, state.Reason, input)
	applyGuidedCleanSelectionOverrides(&next, overrides)
	return next, fmt.Sprintf("age threshold set to %s", guidedAgeString(age))
}

func guidedCleanSelectionOverrides(state guidedCleanState) map[string]bool {
	overrides := make(map[string]bool)
	for _, row := range state.Rows {
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
		if !ok || state.Rows[i].Policy == guidedCleanPolicyLocked {
			continue
		}
		state.Rows[i].Selected = selected
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
