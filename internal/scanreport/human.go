package scanreport

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

// WriteHuman renders the human scan report from View.
func WriteHuman(w io.Writer, view View) {
	fmt.Fprintln(w, "summary")
	if view.Partial {
		fmt.Fprintln(w, "  completeness partial (results are incomplete)")
		for _, providerErr := range view.ProviderErrors {
			fmt.Fprintf(w, "  failed      %-12s %s\n", providerErr.Tool, providerErr.Message)
		}
	}
	writeScanHeadline(w, view)
	fmt.Fprintf(w, "  found       %d %s\n", view.TotalCount, ItemNoun(view.TotalCount))
	fmt.Fprintf(w, "  found size  %s\n", cleaner.FormatSize(view.PhysicalTotalBytes))
	WriteVolumePressure(w, view.Volume)
	if view.TotalStrippableBytes > 0 {
		fmt.Fprintf(w, "  strippable  %s regenerable subtrees inside worktrees (clean --strip)\n",
			cleaner.FormatSize(view.TotalStrippableBytes))
	}
	if view.Partial {
		fmt.Fprintln(w, "  default clean unavailable until a complete scan succeeds")
	} else {
		fmt.Fprintf(w, "  default clean (estimate) %s\n", cleaner.FormatSize(view.DefaultCleanSize))
		WriteCleanupDiagnostics(w, view.DefaultClean, view.Policy)
	}

	WriteHumanExclusions(w, view)
	writeCategorySummary(w, view.ByCategory)
	writeLargestItems(w, view.Items)
	WriteRetention(w, view.Retention)
	writeCodexActivity(w, view.CodexActivity)
	writeDiagnostics(w, view.Diagnostics)
	WriteNext(w, view)
}

func writeScanHeadline(w io.Writer, view View) {
	if view.Partial {
		return
	}
	WriteHeadline(w, view.PhysicalTotalBytes, view.ReclaimPaths, view.Volume)
}

// WriteHeadline prints the one-line scan summary and optional pressure hint.
func WriteHeadline(w io.Writer, found int64, paths []ReclaimPath, report *volume.Report) {
	fmt.Fprintln(w, Headline(found, paths, report))
	writePressureHint(w, paths, report)
}

func writePressureHint(w io.Writer, paths []ReclaimPath, report *volume.Report) {
	if report == nil || (report.Band != volume.BandLow && report.Band != volume.BandCritical) {
		return
	}
	pressure := SizeByLabel(paths, "pressure caches")
	if pressure <= 0 {
		return
	}
	if largest, ok := LargestNonDefault(paths); ok && largest.Flag() == "--pressure" {
		return
	}
	fmt.Fprintf(w, "  reclaim --pressure %s\n", cleaner.FormatSize(pressure))
}

// WriteNext prints the reclaim ladder, review-only line, and scan --json hint.
func WriteNext(w io.Writer, view View) {
	fmt.Fprintln(w, "\nnext")
	if view.Partial {
		fmt.Fprintln(w, "  retry aibris scan; cleanup is disabled for this result")
	} else {
		writeReclaimLadder(w, view.ReclaimPaths)
	}
	WriteReviewOnlyLine(w, view.ReviewOnly.Count, view.ReviewOnly.Size)
	fmt.Fprintln(w, "  aibris scan --json")
}

// WriteReviewOnlyLine prints the next-section review-only worktree summary.
func WriteReviewOnlyLine(w io.Writer, n int, size int64) {
	if n == 0 {
		return
	}
	fmt.Fprintf(w, "  review-only worktrees  %d %s  %s   not a clean/--strip target; inspect mixed/missing .git markers in owner directories\n",
		n, reviewOnlyNoun(n), cleaner.FormatSize(size))
}

func reviewOnlyNoun(n int) string {
	if n == 1 {
		return "unit"
	}
	return "units"
}

func writeReclaimLadder(w io.Writer, paths []ReclaimPath) {
	for _, path := range paths {
		fmt.Fprintf(w, "  %-20s %10s   %s\n", path.Label, cleaner.FormatSize(path.Size), path.Command)
	}
}

// WriteVolumePressure prints the home-volume pressure lines.
func WriteVolumePressure(w io.Writer, report *volume.Report) {
	if report == nil {
		return
	}
	fmt.Fprintf(w, "  volume     %s  %s  %.0f%% used   %s free   %s\n",
		report.Role, report.FSType, report.UsedPercent,
		cleaner.FormatSize(int64(report.AvailableBytes)), volume.HumanWord(report.Band))
	if report.OtherVolumeDebrisBytes > 0 {
		fmt.Fprintf(w, "  debris     %s on this volume   %s other volumes\n",
			cleaner.FormatSize(report.DebrisBytes),
			cleaner.FormatSize(report.OtherVolumeDebrisBytes))
		return
	}
	fmt.Fprintf(w, "  debris     %s on this volume\n", cleaner.FormatSize(report.DebrisBytes))
}

// WriteHumanExclusions prints discovery-only exclusion diagnostics.
func WriteHumanExclusions(w io.Writer, view View) {
	if view.ExcludedByUser == 0 && len(view.ExcludedScopes) == 0 && len(view.RejectedExcludes) == 0 {
		return
	}

	flagPatterns, filePatterns := excludeSourceCounts(view)
	fmt.Fprintln(w, "\nexclusions (discovery only)")
	fmt.Fprintf(w, "  patterns  %d flag, %d ignore-file\n", flagPatterns, filePatterns)
	if view.ExcludedByUser > 0 {
		fmt.Fprintf(w, "  excluded  %d %s hidden from discovery\n", view.ExcludedByUser, ItemNoun(view.ExcludedByUser))
	}

	home := ""
	if userHome, err := os.UserHomeDir(); err == nil {
		home = resolvedDisplayHome(userHome)
	}
	for _, scope := range view.ExcludedScopes {
		display := scope.Resolved
		if home != "" {
			display = DisplayHomePath(home, scope.Resolved)
		}
		fmt.Fprintf(w, "  scope     %-11s %s  %d %s\n", scope.Source, display, scope.Count, ItemNoun(scope.Count))
	}
	for _, rejected := range view.RejectedExcludes {
		fmt.Fprintf(w, "  rejected  %-11s %s  %s\n", rejected.Source, rejected.Pattern, rejected.Reason)
	}
}

func excludeSourceCounts(view View) (flagPatterns, filePatterns int) {
	for _, scope := range view.ExcludedScopes {
		if scope.Source == types.ExcludeSourceFlag {
			flagPatterns++
		} else {
			filePatterns++
		}
	}
	for _, rejected := range view.RejectedExcludes {
		if rejected.Source == types.ExcludeSourceFlag {
			flagPatterns++
		} else {
			filePatterns++
		}
	}
	return flagPatterns, filePatterns
}

func resolvedDisplayHome(home string) string {
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		return resolved
	}
	return home
}

// DisplayHomePath rewrites path as ~/rel when it is inside home.
func DisplayHomePath(home, path string) string {
	rel, err := filepath.Rel(home, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "~"
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return path
	}
	return filepath.Join("~", rel)
}

// WriteRetention prints the read-only retention projection.
func WriteRetention(w io.Writer, projection types.RetentionProjection) {
	if len(projection.Buckets) == 0 && !projection.Partial {
		return
	}

	fmt.Fprintln(w, "\nretention (protected content, read-only)")
	if projection.Partial {
		fmt.Fprintln(w, "  completeness partial (retention inventory only)")
		for _, providerErr := range projection.ProviderErrors {
			fmt.Fprintf(w, "  failed      %-16s %s\n", providerErr.StoreID, providerErr.Message)
		}
	}
	for _, bucket := range projection.Buckets {
		fmt.Fprintf(w, "  %-16s %7s  units %d  members %d  %s  orphaned %d/%s\n",
			bucket.StoreID,
			bucket.BucketID,
			bucket.UnitCount,
			bucket.MemberCount,
			cleaner.FormatSize(bucket.ApparentBytes),
			bucket.OrphanedCount,
			cleaner.FormatSize(bucket.OrphanedBytes),
		)
	}
}

func writeDiagnostics(w io.Writer, diagnostics []types.ProviderDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}

	fmt.Fprintln(w, "\ndiagnostics (experimental)")
	for _, diagnostic := range diagnostics {
		line := fmt.Sprintf("  %-12s %-5s %3d %s   %s   %s",
			diagnostic.Tool,
			diagnostic.State,
			diagnostic.Count,
			ItemNoun(diagnostic.Count),
			cleaner.FormatSize(diagnostic.Bytes),
			diagnostic.Duration,
		)
		if diagnostic.Err != "" {
			line += "  " + diagnostic.Err
		}
		fmt.Fprintln(w, line)
	}
}

func writeCodexActivity(w io.Writer, note *CodexActivityNotice) {
	if note == nil {
		return
	}
	fmt.Fprintln(w, "\ncodex activity")
	fmt.Fprintf(w, "  unavailable; %d active Codex %s protected by default\n",
		note.ProtectedCount, codexWorktreeNoun(note.ProtectedCount))
}

func codexWorktreeNoun(count int) string {
	if count == 1 {
		return "worktree"
	}
	return "worktrees"
}

func writeCategorySummary(w io.Writer, summary map[types.Category]types.CategorySummary) {
	if len(summary) == 0 {
		return
	}

	fmt.Fprintln(w, "\nby category")
	for _, category := range sortedCategories(summary) {
		entry := summary[category]
		fmt.Fprintf(w, "  %-13s %3d   %s\n", category, entry.PhysicalUnitCount, cleaner.FormatSize(entry.PhysicalTotalBytes))
	}
}

func writeLargestItems(w io.Writer, items []Item) {
	if len(items) == 0 {
		return
	}

	limit := 5
	if len(items) < limit {
		limit = len(items)
	}

	fmt.Fprintln(w, "\nlargest")
	for _, item := range items[:limit] {
		fmt.Fprintf(w, "  %8s  %-13s %-12s %-18s %s\n",
			cleaner.FormatSize(item.Size),
			item.Category,
			itemName(item),
			itemProject(item),
			itemAgeAndStatus(item))
	}
	if len(items) > limit {
		fmt.Fprintf(w, "  + %d more\n", len(items)-limit)
	}
}

func sortedCategories(summary map[types.Category]types.CategorySummary) []types.Category {
	categories := make([]types.Category, 0, len(summary))
	for category := range summary {
		categories = append(categories, category)
	}
	sort.Slice(categories, func(i, j int) bool {
		left := summary[categories[i]]
		right := summary[categories[j]]
		if left.Size == right.Size {
			return categories[i] < categories[j]
		}
		return left.Size > right.Size
	})
	return categories
}

// WriteCleanupDiagnostics prints default-clean blocked-reason buckets.
func WriteCleanupDiagnostics(w io.Writer, summary CleanupProjection, opts types.PruneOptions) {
	if summary.ActiveCount > 0 {
		fmt.Fprintf(w, "  protected   %s active worktrees; use --include-active-worktrees after review\n",
			cleaner.FormatSize(summary.ActiveSize))
	}
	if summary.AgeCount > 0 {
		fmt.Fprintf(w, "  age-blocked %s younger than %s\n",
			cleaner.FormatSize(summary.AgeSize), CleanAgeDisplay(opts.Age))
	}
	if summary.RiskyCount > 0 {
		fmt.Fprintf(w, "  risky       %s requires --risky\n", cleaner.FormatSize(summary.RiskySize))
	}
	if summary.FilterCount > 0 && (len(opts.Categories) > 0 || len(opts.Tools) > 0) {
		fmt.Fprintf(w, "  filtered    %s outside category/tool filters\n", cleaner.FormatSize(summary.FilterSize))
	}
	if summary.AgentStateLiveCount > 0 {
		fmt.Fprintf(w, "  agent-state %s %s\n",
			cleaner.FormatSize(summary.AgentStateLiveSize), cleaner.EligibilityReasonAgentStateLive)
	}
	if summary.AgentStateUndeterminedCount > 0 {
		fmt.Fprintf(w, "  agent-state %s %s\n",
			cleaner.FormatSize(summary.AgentStateUndeterminedSize), cleaner.EligibilityReasonAgentStateUndetermined)
	}
	if len(summary.OtherBlocked) > 0 {
		reasons := make([]string, 0, len(summary.OtherBlocked))
		for reason := range summary.OtherBlocked {
			reasons = append(reasons, string(reason))
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			bucket := summary.OtherBlocked[cleaner.EligibilityReason(reason)]
			fmt.Fprintf(w, "  blocked     %s %s\n", cleaner.FormatSize(bucket.Size), reason)
		}
	}
}

func itemName(item Item) string {
	return ItemName(item.debrisInfo())
}

func itemProject(item Item) string {
	return ItemProject(item.debrisInfo())
}

func itemAgeAndStatus(item Item) string {
	return ItemAgeAndStatus(item.debrisInfo())
}
