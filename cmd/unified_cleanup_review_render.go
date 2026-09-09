package cmd

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

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
		if numbered.Row.Relation != CleanupPlanRelationOwner {
			evidenceReason := string(numbered.Row.Relation) + " evidence"
			if reason == "" {
				reason = evidenceReason
			} else {
				reason = evidenceReason + "; " + reason
			}
		}
		line := fmt.Sprintf("  %s %2s  %8s  %-12s  %s",
			checkbox,
			number,
			cleaner.FormatSize(numbered.Row.PhysicalBytes),
			numbered.Row.Item.Category,
			itemName(numbered.Row.Item))
		if reason != "" {
			line += " — " + reason
		}
		fmt.Fprintln(output, truncateCleanupReviewLine(line, width))
	}
}

func cleanupPlanReasonText(reasons []CleanupPlanReason) string {
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		text := reason.Description
		if text == "" {
			continue
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
