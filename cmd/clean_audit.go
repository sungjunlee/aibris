package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

type scanSourceKind string

const (
	scanSourceLive   scanSourceKind = "live"
	scanSourceCached scanSourceKind = "cached"
)

type scanSource struct {
	Kind scanSourceKind
	Age  time.Duration
}

type cleanAudit struct {
	Source             scanSource
	ScannedSources     int
	TotalFoundCount    int
	TotalFoundSize     int64
	TotalEligibleCount int
	TotalEligibleSize  int64
	TotalBlockedCount  int
	TotalBlockedSize   int64
	Categories         []cleanAuditCategory
}

type cleanAuditCategory struct {
	Category      types.Category
	FoundCount    int
	FoundSize     int64
	EligibleCount int
	EligibleSize  int64
	BlockedCount  int
	BlockedSize   int64
	MainReason    string
}

type cleanAuditReason string

const (
	cleanReasonFiltered       cleanAuditReason = "outside category/tool filters"
	cleanReasonRisky          cleanAuditReason = "requires --risky"
	cleanReasonActiveWorktree cleanAuditReason = "active worktree protected"
	cleanReasonAge            cleanAuditReason = "younger than configured age"
	cleanReasonMissingPath    cleanAuditReason = "path no longer exists"
	cleanReasonEligible       cleanAuditReason = "eligible for cleanup"
)

type cleanAuditReasonStat struct {
	Count int
	Size  int64
}

func buildCleanAudit(items, targets []types.DebrisInfo, opts types.PruneOptions, scannedSources int, source scanSource) cleanAudit {
	targetKeys := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetKeys[cleanAuditItemKey(target)] = true
	}

	byCategory := make(map[types.Category]*cleanAuditCategory)
	reasonsByCategory := make(map[types.Category]map[cleanAuditReason]cleanAuditReasonStat)
	audit := cleanAudit{Source: source, ScannedSources: scannedSources}

	for _, item := range items {
		category := item.Category
		row := byCategory[category]
		if row == nil {
			row = &cleanAuditCategory{Category: category}
			byCategory[category] = row
		}

		row.FoundCount++
		row.FoundSize += item.Size
		audit.TotalFoundCount++
		audit.TotalFoundSize += item.Size

		reason := cleanAuditBlockReason(item, opts, targetKeys[cleanAuditItemKey(item)])
		if reason == cleanReasonEligible {
			row.EligibleCount++
			row.EligibleSize += item.Size
			audit.TotalEligibleCount++
			audit.TotalEligibleSize += item.Size
			continue
		}

		row.BlockedCount++
		row.BlockedSize += item.Size
		audit.TotalBlockedCount++
		audit.TotalBlockedSize += item.Size
		if reasonsByCategory[category] == nil {
			reasonsByCategory[category] = make(map[cleanAuditReason]cleanAuditReasonStat)
		}
		stat := reasonsByCategory[category][reason]
		stat.Count++
		stat.Size += item.Size
		reasonsByCategory[category][reason] = stat
	}

	for category, row := range byCategory {
		row.MainReason = cleanAuditMainReason(*row, reasonsByCategory[category], opts)
		audit.Categories = append(audit.Categories, *row)
	}
	sort.Slice(audit.Categories, func(i, j int) bool {
		left := audit.Categories[i]
		right := audit.Categories[j]
		if left.FoundSize == right.FoundSize {
			return left.Category < right.Category
		}
		return left.FoundSize > right.FoundSize
	})

	return audit
}

func cleanAuditItemKey(item types.DebrisInfo) string {
	return string(item.Category) + "\x00" + string(item.Tool) + "\x00" + item.ID + "\x00" + item.Path
}

func cleanAuditBlockReason(item types.DebrisInfo, opts types.PruneOptions, isTarget bool) cleanAuditReason {
	if !cmdContainsCategory(opts.Categories, item.Category) || !cmdContainsTool(opts.Tools, item.Tool) {
		return cleanReasonFiltered
	}
	if !opts.Risky && item.Category.IsRisky() {
		return cleanReasonRisky
	}
	if !opts.IncludeActiveWorktrees && item.Category == types.CategoryWorktree && item.Status == types.WorktreeActive {
		return cleanReasonActiveWorktree
	}
	if !item.ModTime.Before(time.Now().Add(-opts.Age)) {
		return cleanReasonAge
	}
	if !isTarget {
		return cleanReasonMissingPath
	}
	return cleanReasonEligible
}

func cleanAuditMainReason(row cleanAuditCategory, stats map[cleanAuditReason]cleanAuditReasonStat, opts types.PruneOptions) string {
	if row.BlockedCount == 0 {
		return string(cleanReasonEligible)
	}
	var best cleanAuditReason
	var bestStat cleanAuditReasonStat
	for reason, stat := range stats {
		if best == "" || stat.Size > bestStat.Size || (stat.Size == bestStat.Size && stat.Count > bestStat.Count) {
			best = reason
			bestStat = stat
		}
	}
	return cleanAuditReasonText(best, opts)
}

func cleanAuditReasonText(reason cleanAuditReason, opts types.PruneOptions) string {
	switch reason {
	case cleanReasonAge:
		return "younger than " + cleanAgeDisplay(opts.Age)
	case cleanReasonRisky:
		return "requires --risky"
	case cleanReasonActiveWorktree:
		return "active worktree protected"
	case cleanReasonFiltered:
		return "outside category/tool filters"
	case cleanReasonMissingPath:
		return "path no longer exists"
	default:
		return string(reason)
	}
}

func cleanAuditPolicyLine(opts types.PruneOptions) string {
	activePolicy := "protected"
	if opts.IncludeActiveWorktrees {
		activePolicy = "included"
	}
	return fmt.Sprintf("age>=%s, risky=%t, active-worktrees=%s", cleanAgeDisplay(opts.Age), opts.Risky, activePolicy)
}

func cleanAuditScanSourceLine(source scanSource) string {
	if source.Kind == scanSourceCached {
		return fmt.Sprintf("cached, %s old", shortDurationString(source.Age))
	}
	return "live"
}

func printCleanAudit(audit cleanAudit, opts types.PruneOptions) {
	fmt.Printf("  policy  %s\n", cleanAuditPolicyLine(opts))
	fmt.Printf("  scan    %s\n\n", cleanAuditScanSourceLine(audit.Source))

	fmt.Println("scan summary")
	fmt.Printf("  scanned    %d sources   %d %s   %s\n",
		audit.ScannedSources, audit.TotalFoundCount, itemNoun(audit.TotalFoundCount), cleaner.FormatSize(audit.TotalFoundSize))
	fmt.Printf("  eligible   %d %s   %s\n",
		audit.TotalEligibleCount, itemNoun(audit.TotalEligibleCount), cleaner.FormatSize(audit.TotalEligibleSize))
	fmt.Printf("  protected/skipped %d %s   %s\n\n",
		audit.TotalBlockedCount, itemNoun(audit.TotalBlockedCount), cleaner.FormatSize(audit.TotalBlockedSize))

	if len(audit.Categories) == 0 {
		return
	}
	fmt.Println("by category")
	fmt.Printf("  %-13s %12s %12s %18s  %s\n", "category", "found", "eligible", "protected/skipped", "main reason")
	for _, row := range audit.Categories {
		fmt.Printf("  %-13s %3d %8s %3d %8s %3d %8s  %s\n",
			row.Category,
			row.FoundCount, cleaner.FormatSize(row.FoundSize),
			row.EligibleCount, cleaner.FormatSize(row.EligibleSize),
			row.BlockedCount, cleaner.FormatSize(row.BlockedSize),
			row.MainReason)
	}
	fmt.Println()
}

func printCleanupReceipt(targetCount int, freed int64, audit cleanAudit) {
	fmt.Println()
	fmt.Println("cleanup receipt")
	fmt.Printf("  targets    %d %s\n", targetCount, itemNoun(targetCount))
	fmt.Printf("  freed      %s\n", cleaner.FormatSize(freed))
	fmt.Printf("  protected/skipped %d %s   %s\n",
		audit.TotalBlockedCount, itemNoun(audit.TotalBlockedCount), cleaner.FormatSize(audit.TotalBlockedSize))
}

func cleanTargetReason(w types.DebrisInfo) string {
	reason := itemReason(w)
	return strings.TrimSuffix(reason, "; protected from cleanup by default")
}
