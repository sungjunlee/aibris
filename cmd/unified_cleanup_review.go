package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

type cleanupReviewMode string

const (
	cleanupReviewText cleanupReviewMode = "text"
	cleanupReviewTTY  cleanupReviewMode = "tty checklist"
)

const (
	cleanupReviewNarrowWidth = 72
	cleanupReviewWideWidth   = 120
)

type numberedCleanupPlanRow struct {
	Number int
	Row    CleanupPlanRow
}

// promptUnifiedCleanupReview lets text and TTY frontends mutate the same plan
// state. Rendering is deliberately separate from execution; #115 wires the
// accepted selection through preflight, confirmation, and receipts.
func promptUnifiedCleanupReview(input io.Reader, output io.Writer, plan UnifiedCleanupPlan, mode cleanupReviewMode, width int) (UnifiedCleanupPlan, bool, error) {
	scanner := bufio.NewScanner(input)
	status := ""
	for {
		renderUnifiedCleanupReview(output, plan, status, mode, width)
		fmt.Fprint(output, "\nEnter numbers to toggle, Enter to preview, q to abort: ")
		status = ""
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return plan, false, err
			}
			return plan, false, nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			return plan, false, nil
		}
		if strings.EqualFold(line, "q") {
			fmt.Fprintln(output, "Aborted.")
			return plan, true, nil
		}
		numbers, ok := parseCleanupReviewNumbers(line)
		if !ok {
			status = "enter row numbers separated by spaces, or q"
			continue
		}
		next := plan
		toggled := 0
		seen := make(map[int]bool)
		for _, number := range numbers {
			if seen[number] {
				continue
			}
			seen[number] = true
			var changed bool
			next, changed = toggleUnifiedCleanupPlanRow(next, number)
			if changed {
				toggled++
			}
		}
		if toggled == 0 {
			status = "no selectable rows matched"
			continue
		}
		plan = next
		status = fmt.Sprintf("updated %d %s", toggled, itemNoun(toggled))
	}
}

func renderUnifiedCleanupReview(output io.Writer, plan UnifiedCleanupPlan, status string, mode cleanupReviewMode, width int) {
	if width <= 0 {
		width = cleanupReviewWideWidth
	}
	totals := plan.Totals()
	fmt.Fprintln(output, "cleanup review")
	if mode == cleanupReviewTTY {
		fmt.Fprintf(output, "  mode       %s\n", mode)
	}
	if status != "" {
		fmt.Fprintf(output, "  status     %s\n", status)
	}
	fmt.Fprintln(output)
	fmt.Fprintln(output, "summary")
	fmt.Fprintf(output, "  found      %d %s   %s\n", totals.PhysicalTargets, itemNoun(totals.PhysicalTargets), cleaner.FormatSize(totals.PhysicalBytes))
	fmt.Fprintf(output, "  eligible   %d %s   %s\n", totals.EligibleTargets, itemNoun(totals.EligibleTargets), cleaner.FormatSize(totals.EligibleBytes))
	fmt.Fprintf(output, "  selected   %d %s   %s\n", totals.SelectedTargets, itemNoun(totals.SelectedTargets), cleaner.FormatSize(totals.SelectedBytes))
	fmt.Fprintf(output, "  reviewable %d %s   %s\n", totals.ReviewableTargets, itemNoun(totals.ReviewableTargets), cleaner.FormatSize(totals.ReviewableBytes))
	fmt.Fprintf(output, "  protected  %d %s   %s\n", totals.HardLockedTargets, itemNoun(totals.HardLockedTargets), cleaner.FormatSize(totals.HardLockedBytes))

	rows := numberedCleanupPlanRows(plan)
	renderCleanupReviewSection(output, "selected for cleanup", rows, CleanupPlanSelected, width)
	renderCleanupReviewSection(output, "review before cleanup", rows, CleanupPlanUnselected, width)
	renderCleanupReviewSection(output, "protected", rows, CleanupPlanLocked, width)
	if len(rows) == 0 {
		fmt.Fprintln(output, "\nNo cleanup candidates.")
	}
}

func renderCleanupReviewSection(output io.Writer, title string, rows []numberedCleanupPlanRow, selection CleanupPlanSelection, width int) {
	var matching []numberedCleanupPlanRow
	for _, row := range rows {
		if row.Row.Selection == selection {
			matching = append(matching, row)
		}
	}
	if len(matching) == 0 {
		return
	}
	fmt.Fprintf(output, "\n%s\n", title)
	for _, numbered := range matching {
		checkbox := "[ ]"
		number := fmt.Sprintf("%d", numbered.Number)
		switch numbered.Row.Selection {
		case CleanupPlanSelected:
			checkbox = "[x]"
		case CleanupPlanLocked:
			checkbox = "[!]"
			number = "-"
		}
		reason := cleanupPlanReasonText(numbered.Row.Reasons)
		line := fmt.Sprintf("  %s %2s  %8s  %-12s  %s",
			checkbox,
			number,
			cleaner.FormatSize(numbered.Row.Item.Size),
			numbered.Row.Item.Category,
			itemName(numbered.Row.Item))
		if reason != "" {
			line += " — " + reason
		}
		fmt.Fprintln(output, truncateCleanupReviewLine(line, width))
	}
}

func numberedCleanupPlanRows(plan UnifiedCleanupPlan) []numberedCleanupPlanRow {
	rows := make([]numberedCleanupPlanRow, 0, len(plan.Rows))
	number := 0
	numbersByTarget := make(map[string]int)
	for _, row := range plan.Rows {
		if row.Selection != CleanupPlanLocked {
			existing := numbersByTarget[row.TargetKey]
			if existing == 0 {
				number++
				existing = number
				numbersByTarget[row.TargetKey] = existing
			}
			rows = append(rows, numberedCleanupPlanRow{Number: existing, Row: row})
			continue
		}
		rows = append(rows, numberedCleanupPlanRow{Row: row})
	}
	return rows
}

func toggleUnifiedCleanupPlanRow(plan UnifiedCleanupPlan, number int) (UnifiedCleanupPlan, bool) {
	if number <= 0 {
		return plan, false
	}
	rows := numberedCleanupPlanRows(plan)
	var targetKey string
	for _, row := range rows {
		if row.Number == number && row.Row.Selection != CleanupPlanLocked {
			targetKey = row.Row.TargetKey
			break
		}
	}
	if targetKey == "" {
		return plan, false
	}

	next := cloneUnifiedCleanupPlan(plan)
	var selection CleanupPlanSelection
	for i := range next.Targets {
		if next.Targets[i].Key != targetKey || next.Targets[i].Selection == CleanupPlanLocked {
			continue
		}
		if next.Targets[i].Selection == CleanupPlanSelected {
			selection = CleanupPlanUnselected
		} else {
			selection = CleanupPlanSelected
		}
		next.Targets[i].Selection = selection
	}
	if selection == "" {
		return plan, false
	}
	for i := range next.Rows {
		if next.Rows[i].TargetKey == targetKey {
			next.Rows[i].Selection = selection
		}
	}
	return next, true
}

func cloneUnifiedCleanupPlan(plan UnifiedCleanupPlan) UnifiedCleanupPlan {
	next := plan
	next.Rows = append([]CleanupPlanRow(nil), plan.Rows...)
	for i := range next.Rows {
		next.Rows[i].Reasons = append([]CleanupPlanReason(nil), plan.Rows[i].Reasons...)
	}
	next.Targets = append([]CleanupPhysicalTarget(nil), plan.Targets...)
	for i := range next.Targets {
		next.Targets[i].RowKeys = append([]string(nil), plan.Targets[i].RowKeys...)
	}
	next.Evidence.ProviderErrors = append([]types.ScanProviderError(nil), plan.Evidence.ProviderErrors...)
	return next
}

func parseCleanupReviewNumbers(line string) ([]int, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, false
	}
	numbers := make([]int, 0, len(fields))
	for _, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil || number <= 0 {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, true
}

func cleanupPlanReasonText(reasons []CleanupPlanReason) string {
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		text := reason.Description
		if text == "" {
			text = string(reason.Code)
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "; ")
}

func truncateCleanupReviewLine(line string, width int) string {
	if width <= 0 || utf8.RuneCountInString(line) <= width {
		return line
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(line)
	return string(runes[:width-1]) + "…"
}
