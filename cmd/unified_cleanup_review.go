package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

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

func numberedCleanupPlanRows(plan UnifiedCleanupPlan) []numberedCleanupPlanRow {
	rows := make([]numberedCleanupPlanRow, 0, len(plan.Rows))
	number := 0
	numbersByOwner := make(map[string]int)
	for _, row := range plan.Rows {
		if row.Selection != CleanupPlanLocked {
			existing := numbersByOwner[row.OwnerKey]
			if existing == 0 {
				number++
				existing = number
				numbersByOwner[row.OwnerKey] = existing
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
	var ownerKey string
	for _, row := range rows {
		if row.Number == number && row.Row.Selection != CleanupPlanLocked {
			ownerKey = row.Row.OwnerKey
			break
		}
	}
	if ownerKey == "" {
		return plan, false
	}

	next := cloneUnifiedCleanupPlan(plan)
	var selection CleanupPlanSelection
	for i := range next.Components {
		if next.Components[i].Key != ownerKey ||
			next.Components[i].Selection == CleanupPlanLocked {
			continue
		}
		if next.Components[i].Selection == CleanupPlanSelected {
			selection = CleanupPlanUnselected
		} else {
			selection = CleanupPlanSelected
		}
		next.Components[i].Selection = selection
	}
	if selection == "" {
		return plan, false
	}
	for i := range next.Targets {
		if next.Targets[i].OwnerKey == ownerKey {
			next.Targets[i].Selection = selection
		}
	}
	for i := range next.Rows {
		if next.Rows[i].OwnerKey == ownerKey {
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
	next.Components = append([]CleanupPhysicalComponent(nil), plan.Components...)
	for i := range next.Components {
		next.Components[i].TargetKeys = append([]string(nil), plan.Components[i].TargetKeys...)
		next.Components[i].RowKeys = append([]string(nil), plan.Components[i].RowKeys...)
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
