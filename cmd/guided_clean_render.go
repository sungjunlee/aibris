package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

const guidedProtectedDisplayLimit = 20

type guidedCleanPromptMode string

const (
	guidedCleanPromptText guidedCleanPromptMode = "text"
	guidedCleanPromptTTY  guidedCleanPromptMode = "tty checklist"
)

func promptGuidedCleanForFiles(input *os.File, output *os.File, state guidedCleanState) ([]types.DebrisInfo, bool, error) {
	if isTerminal(input) && isTerminal(output) {
		return promptGuidedCleanWithMode(input, output, state, guidedCleanPromptTTY)
	}
	return promptGuidedClean(input, output, state)
}

// promptGuidedCleanStateForFiles returns the accepted selection state so the
// unified cleanup plan can reuse the same policy decisions and toggles.
func promptGuidedCleanStateForFiles(input *os.File, output *os.File, state guidedCleanState) (guidedCleanState, bool, error) {
	if isTerminal(input) && isTerminal(output) {
		return promptGuidedCleanStateWithMode(input, output, state, guidedCleanPromptTTY)
	}
	return promptGuidedCleanStateWithMode(input, output, state, guidedCleanPromptText)
}

func promptGuidedClean(input io.Reader, output io.Writer, state guidedCleanState) ([]types.DebrisInfo, bool, error) {
	return promptGuidedCleanWithMode(input, output, state, guidedCleanPromptText)
}

func promptGuidedCleanWithMode(input io.Reader, output io.Writer, state guidedCleanState, mode guidedCleanPromptMode) ([]types.DebrisInfo, bool, error) {
	final, aborted, err := promptGuidedCleanStateWithMode(input, output, state, mode)
	if err != nil || aborted {
		return nil, aborted, err
	}
	return selectedGuidedCleanTargets(final), false, nil
}

// promptGuidedCleanStateWithMode returns the accepted selection state so the
// unified cleanup plan can reuse the same policy decisions and toggles across
// every category instead of a separate guided-then-classic handoff.
func promptGuidedCleanStateWithMode(input io.Reader, output io.Writer, state guidedCleanState, mode guidedCleanPromptMode) (guidedCleanState, bool, error) {
	scanner := bufio.NewScanner(input)
	status := ""
	for {
		renderGuidedClean(output, state, status, mode)
		status = ""
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return guidedCleanState{}, false, err
			}
			return state, false, nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			return state, false, nil
		}
		if strings.EqualFold(line, "q") {
			fmt.Fprintln(output, "Aborted.")
			return guidedCleanState{}, true, nil
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
