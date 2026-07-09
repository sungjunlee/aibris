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

type guidedCleanRow struct {
	Number    int
	Row       guidedCodexWorktreeRow
	Selected  bool
	Protected bool
}

type guidedCleanState struct {
	ScanSource scanSource
	Reason     string
	Activity   codexActivityIndex
	Rows       []guidedCleanRow
}

func buildGuidedCleanState(ctx context.Context, result *types.ScanResult, source scanSource, age time.Duration, reason string) guidedCleanState {
	activity := loadCodexActivityIndex(ctx)
	gitSafety := inspectGuidedCodexGitSafety(ctx, result.Worktrees)
	cwd, _ := os.Getwd()
	plan := buildGuidedCodexWorktreePlan(guidedCodexWorktreePlanInput{
		Worktrees:               result.Worktrees,
		Activity:                activity,
		GitSafety:               gitSafety,
		CurrentWorkingDirectory: cwd,
		Age:                     age,
	})
	return newGuidedCleanState(source, reason, activity, plan)
}

func runGuidedCodexClean(opts types.PruneOptions, state guidedCleanState) error {
	targets, aborted, err := promptGuidedClean(os.Stdin, os.Stdout, state)
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

	if opts.Interactive {
		total := interactiveClean(targets)
		fmt.Printf("Cleaned %d items, freed %s\n", len(targets), cleaner.FormatSize(total))
		return nil
	}
	if !opts.Force {
		if !confirmCleanExecution() {
			return nil
		}
	}
	total, err := cleaner.Execute(targets)
	if err != nil {
		return err
	}
	fmt.Printf("Cleaned %d items, freed %s\n", len(targets), cleaner.FormatSize(total))
	return nil
}

func inspectGuidedCodexGitSafety(ctx context.Context, items []types.DebrisInfo) map[string]worktreeGitSafety {
	safety := make(map[string]worktreeGitSafety)
	for _, item := range activeCodexWorktrees(items) {
		safety[item.Path] = inspectWorktreeGitState(ctx, item.Path)
		if path, ok := cleanTargetPathKey(item.Path); ok {
			safety[path] = safety[item.Path]
		}
	}
	return safety
}

func newGuidedCleanState(source scanSource, reason string, activity codexActivityIndex, plan guidedCodexWorktreePlan) guidedCleanState {
	rows := make([]guidedCleanRow, 0, len(plan.Selected)+len(plan.Protected))
	number := 1
	for _, row := range plan.Selected {
		rows = append(rows, guidedCleanRow{Number: number, Row: row, Selected: true})
		number++
	}
	for _, row := range plan.Protected {
		rows = append(rows, guidedCleanRow{Number: number, Row: row, Protected: true})
		number++
	}
	return guidedCleanState{ScanSource: source, Reason: reason, Activity: activity, Rows: rows}
}

func promptGuidedClean(input io.Reader, output io.Writer, state guidedCleanState) ([]types.DebrisInfo, bool, error) {
	scanner := bufio.NewScanner(input)
	for {
		renderGuidedClean(output, state)
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
		for _, field := range strings.FieldsFunc(line, guidedToggleSeparator) {
			if field == "" {
				continue
			}
			n, err := strconv.Atoi(field)
			if err != nil {
				continue
			}
			toggleGuidedCleanRow(&state, n)
		}
	}
}

func guidedToggleSeparator(r rune) bool {
	return r == ',' || r == ' ' || r == '\t'
}

func toggleGuidedCleanRow(state *guidedCleanState, number int) {
	for i := range state.Rows {
		if state.Rows[i].Number == number {
			state.Rows[i].Selected = !state.Rows[i].Selected
			return
		}
	}
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

func hasSelectedGuidedCleanTargets(state guidedCleanState) bool {
	return len(selectedGuidedCleanTargets(state)) > 0
}

func renderGuidedClean(output io.Writer, state guidedCleanState) {
	selectedCount, selectedSize := guidedSelectionTotals(state)
	protectedRows := guidedProtectedRows(state)
	protectedCount, protectedSize := guidedProtectedTotals(state)

	fmt.Fprintln(output, "guided codex worktree cleanup")
	if state.Reason != "" {
		fmt.Fprintf(output, "  reason     %s\n", state.Reason)
	}
	fmt.Fprintf(output, "\nscan\n  source     %s\n  activity   %s\n", cleanAuditScanSourceLine(state.ScanSource), guidedActivitySourceLine(state.Activity))
	fmt.Fprintf(output, "\nsummary\n  selected   %d %s   %s\n  protected  %d %s   %s\n",
		selectedCount, itemNoun(selectedCount), cleaner.FormatSize(selectedSize),
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
	fmt.Fprint(output, "\nEnter numbers to toggle, Enter to preview, q to abort: ")
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
