package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func printCleanPlan(targets []types.DebrisInfo, mode cleanPlanMode) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	} else {
		home = resolvedDisplayHome(home)
	}
	var totalSize int64
	for _, w := range targets {
		totalSize += w.Size
	}

	fmt.Println("clean plan")
	fmt.Printf("  mode     %s\n", mode)
	fmt.Printf("  targets  %d %s   %s\n", len(targets), itemNoun(len(targets)), cleaner.FormatSize(totalSize))
	fmt.Println()
	fmt.Println("targets")
	fmt.Printf("  %8s  %-13s %-12s %-18s %-14s %-12s %s\n",
		"size", "category", "name", "project", "age/status", "action", "reason")
	for _, w := range displayCleanPlanTargets(targets, mode) {
		printCleanTarget(w, home)
	}
	fmt.Println()
}

func displayCleanPlanTargets(targets []types.DebrisInfo, mode cleanPlanMode) []types.DebrisInfo {
	if mode != cleanPlanModeDryRun {
		return targets
	}
	displayed := append([]types.DebrisInfo(nil), targets...)
	sort.SliceStable(displayed, func(i, j int) bool {
		if displayed[i].Size != displayed[j].Size {
			return displayed[i].Size > displayed[j].Size
		}
		return cleaner.TargetStableKey(displayed[i]) < cleaner.TargetStableKey(displayed[j])
	})
	return displayed
}

func printCleanPlanWithComponents(
	targets []types.DebrisInfo,
	components []cleanupOverlapComponent,
	mode cleanPlanMode,
) {
	printCleanPlan(targets, mode)
	targetKeys := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetKeys[cleaner.TargetStableKey(target)] = true
	}
	printedHeader := false
	for _, component := range components {
		if !targetKeys[cleaner.TargetStableKey(component.Owner)] ||
			!cleanupComponentHasLineage(component) {
			continue
		}
		if !printedHeader {
			fmt.Println("overlap lineage")
			printedHeader = true
		}
		printCleanupComponentLineage(component, "  ")
	}
	if printedHeader {
		fmt.Println()
	}
}

func cleanupComponentHasLineage(component cleanupOverlapComponent) bool {
	if len(component.LogicalRows) > 1 || len(component.Obligations) > 0 {
		return true
	}
	for _, row := range component.LogicalRows {
		if row.L1Reason != "" {
			return true
		}
	}
	return false
}

func printCleanupComponentLineage(
	component cleanupOverlapComponent,
	indent string,
) {
	fmt.Printf("%sowner     %s   %s\n",
		indent,
		itemName(component.Owner),
		cleaner.FormatSize(component.Owner.Size))
	for _, row := range component.LogicalRows {
		classification := ""
		if row.Item.Classification != "" {
			classification = " classification=" + string(row.Item.Classification)
		}
		displayPath := cleanupLogicalDisplayPath(component, row)
		fmt.Printf("%sevidence  %-19s %-13s %-12s%s %s\n",
			indent+"  ",
			row.Relation,
			row.Item.Category,
			row.Item.Tool,
			classification,
			displayPath)
		if row.PolicyReason != "" {
			fmt.Printf("%s  policy   %s\n", indent+"  ", row.PolicyReason)
		}
		if row.L1Reason != "" {
			fmt.Printf("%s  overlap  %s\n", indent+"  ", row.L1Reason)
		}
	}
}

func cleanupLogicalDisplayPath(
	component cleanupOverlapComponent,
	row cleanupOverlapLogicalRow,
) string {
	switch row.Relation {
	case cleanupOverlapOwner:
		return "[owner target]"
	case cleanupOverlapExact:
		return fmt.Sprintf("[exact discovery %d]", row.DiscoveryOrdinal)
	case cleanupOverlapDescendant:
		rel, err := filepath.Rel(component.CanonicalPath, row.CanonicalPath)
		if err == nil {
			return "." + string(filepath.Separator) + rel
		}
	case cleanupOverlapAncestor:
		return "[containing protected discovery]"
	}
	return row.Item.Path
}

func printCleanTarget(w types.DebrisInfo, home string) {
	fmt.Println(cleanPlanLine(w))
	if home != "" {
		fmt.Printf("    %s\n", displayHomePath(home, w.Path))
	} else {
		fmt.Printf("    %s\n", w.Path)
	}
	if cleanupKind(w) == types.CleanupCommand && len(w.CleanupCommand) > 0 {
		fmt.Printf("    command: %s\n", strings.Join(w.CleanupCommand, " "))
	}
}

func cleanPlanLine(w types.DebrisInfo) string {
	return fmt.Sprintf("  %8s  %-13s %-12s %-18s %-14s %-12s %s",
		cleaner.FormatSize(w.Size),
		w.Category,
		itemName(w),
		itemProject(w),
		itemAgeAndStatus(w),
		cleanAction(w),
		cleanTargetReason(w))
}

func cleanAction(w types.DebrisInfo) string {
	if cleanupKind(w) == types.CleanupCommand && len(w.CleanupCommand) > 0 {
		return string(types.CleanupCommand)
	}
	return string(types.CleanupRemovePath)
}

func resolvedDisplayHome(home string) string {
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		return resolved
	}
	return home
}
