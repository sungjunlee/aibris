package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// printStripCWDRefusals states every unit strip refused because the working
// directory is inside it. A refused unit is reported rather than omitted, so
// a missing candidate never reads as "nothing to reclaim here".
func printStripCWDRefusals(refusedForCWD []types.DebrisInfo, home string) {
	if len(refusedForCWD) == 0 {
		return
	}
	var total int64
	for _, item := range refusedForCWD {
		total += item.StrippableBytes
	}
	fmt.Printf("  refused  %d %s   %s held by current working directory\n",
		len(refusedForCWD), candidateNoun(len(refusedForCWD)), cleaner.FormatSize(total))
	for _, item := range refusedForCWD {
		fmt.Printf("    kept %s — run from outside the worktree to strip it\n",
			displayHomePath(home, item.Path))
	}
}

func printStripProtectRefusals(refused []types.DebrisInfo, home string) {
	if len(refused) == 0 {
		return
	}
	var total int64
	for _, item := range refused {
		total += item.StrippableBytes
	}
	fmt.Printf("  protected %d %s   %s  %s\n",
		len(refused), candidateNoun(len(refused)), cleaner.FormatSize(total), string(cleanReasonProtectPath))
	for _, item := range refused {
		fmt.Printf("    kept %s — %s\n",
			displayHomePath(home, item.Path), string(cleanReasonProtectPath))
	}
}

func printStripUnitOutcome(outcome stripUnitOutcome) {
	fmt.Printf("result: %s (%s) — %s freed\n",
		itemName(outcome.Item), outcome.Item.Category, cleaner.FormatSize(outcome.Freed))
	for _, subtree := range outcome.Subtrees {
		printStripSubtreeLine(subtree)
	}
	if outcome.Error != "" {
		fmt.Printf("  error   %s\n", outcome.Error)
	}
}

func printStripSubtreeLine(subtree stripSubtreeOutcome) {
	if subtree.Skipped != "" {
		fmt.Printf("  kept    %s: %s\n", subtree.Path, subtree.Skipped)
		return
	}
	fmt.Printf("  removed %s — %s\n", subtree.Path, cleaner.FormatSize(subtree.Bytes))
}

type stripCloser struct {
	planned     int
	stripped    int
	freed       int64
	kept        int
	keptBytes   int64
	keepReasons []string
}

func summarizeStripOutcomes(outcomes []stripUnitOutcome, planned int) stripCloser {
	var closer stripCloser
	closer.planned = planned
	if closer.planned < len(outcomes) {
		closer.planned = len(outcomes)
	}
	reasons := make(map[string]struct{})
	for _, outcome := range outcomes {
		accountStripUnit(outcome, &closer, reasons)
	}
	closer.keepReasons = sortedStripReasons(reasons)
	return closer
}

func accountStripUnit(outcome stripUnitOutcome, closer *stripCloser, reasons map[string]struct{}) {
	closer.freed += outcome.Freed
	if outcome.Freed > 0 {
		closer.stripped++
	}
	if !recordStripKeeps(outcome, reasons) {
		return
	}
	closer.kept++
	remaining := outcome.Item.StrippableBytes - outcome.Freed
	if remaining < 0 {
		remaining = 0
	}
	closer.keptBytes += remaining
}

func recordStripKeeps(outcome stripUnitOutcome, reasons map[string]struct{}) bool {
	kept := false
	for _, subtree := range outcome.Subtrees {
		if subtree.Skipped == "" {
			continue
		}
		kept = true
		reasons[subtree.Skipped] = struct{}{}
	}
	return kept
}

func sortedStripReasons(reasons map[string]struct{}) []string {
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func printStripCloser(closer stripCloser) {
	fmt.Printf("\nstripped  %d/%d %s   %s freed\n",
		closer.stripped, closer.planned, unitNoun(closer.planned), cleaner.FormatSize(closer.freed))
	if closer.kept == 0 {
		return
	}
	fmt.Printf("kept      %d %s      %s   %s\n",
		closer.kept, unitNoun(closer.kept), cleaner.FormatSize(closer.keptBytes),
		strings.Join(closer.keepReasons, "; "))
}

func unitNoun(count int) string {
	if count == 1 {
		return "unit"
	}
	return "units"
}
