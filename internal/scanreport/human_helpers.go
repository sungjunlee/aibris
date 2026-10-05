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

// Human report section printers and display helpers.
// WriteHuman orchestration stays in human.go.

// WriteNext prints the commands that act on this scan, largest first after
// the default, plus the review-only line and the JSON hint.
func WriteNext(w io.Writer, view View) {
	fmt.Fprintln(w, "\nnext")
	if view.Partial {
		fmt.Fprintln(w, "  aibris scan                  retry; cleanup is disabled for this result")
	} else {
		writeReclaimLadder(w, view.ReclaimPaths)
	}
	WriteReviewOnlyLine(w, view.ReviewOnly.Count, view.ReviewOnly.Size)
	fmt.Fprintln(w, "  aibris scan --json           machine-readable inventory")
}

// WriteReviewOnlyLine prints the next-section review-only worktree summary.
func WriteReviewOnlyLine(w io.Writer, n int, size int64) {
	if n == 0 {
		return
	}
	fmt.Fprintf(w, "  review by hand               %d worktree %s, %s: mixed or missing .git markers; never cleaned\n",
		n, reviewOnlyNoun(n), cleaner.FormatSize(size))
}

func reviewOnlyNoun(n int) string {
	if n == 1 {
		return "unit"
	}
	return "units"
}

func writeReclaimLadder(w io.Writer, paths []ReclaimPath) {
	width := nextCommandWidth
	for _, path := range paths {
		width = max(width, len(path.Command))
	}
	for _, path := range paths {
		fmt.Fprintf(w, "  %-*s %s %s\n", width, path.Command, cleaner.FormatSize(path.Size), reclaimLadderNote(path))
	}
}

// nextCommandWidth is the minimum command column in the next section; the
// fixed review-only and JSON lines are written to the same width.
const nextCommandWidth = 28

func reclaimLadderNote(path ReclaimPath) string {
	switch path.Label {
	case labelDefaultDelete:
		return "by default"
	case labelStrip:
		return "from inside kept worktrees"
	case labelPressure:
		return "including younger caches"
	default:
		return path.Label
	}
}

// writeCacheRelaxNote explains why young caches count as reclaimable.
func writeCacheRelaxNote(w io.Writer, policy types.PruneOptions) {
	switch {
	case !policy.RelaxCacheAge:
	case policy.PressureDevice != "":
		summaryRow(w, "", "caches on the home volume count at any age (volume nearly full)")
	default:
		summaryRow(w, "", "caches count at any age (--pressure)")
	}
}

// WriteVolumePressure prints the home volume's capacity and, when debris
// spans volumes, how much of it is here.
func WriteVolumePressure(w io.Writer, report *volume.Report) {
	if report == nil {
		return
	}
	summaryRow(w, "volume", "%s (%s): %.0f%% used, %s free, %s",
		report.Role, report.FSType, report.UsedPercent,
		cleaner.FormatSize(int64(report.AvailableBytes)), volume.HumanWord(report.Band))
	if report.OtherVolumeDebrisBytes > 0 {
		summaryRow(w, "", "%s of the debris is here, %s on other volumes",
			cleaner.FormatSize(report.DebrisBytes), cleaner.FormatSize(report.OtherVolumeDebrisBytes))
	}
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

// WriteCleanupDiagnostics prints default-clean blocked-reason buckets.
func WriteCleanupDiagnostics(w io.Writer, summary CleanupProjection, opts types.PruneOptions) {
	label := "held back"
	row := func(size int64, format string, args ...any) {
		summaryRow(w, label, "%s %s", cleaner.FormatSize(size), fmt.Sprintf(format, args...))
		label = ""
	}
	if summary.AgeCount > 0 {
		row(summary.AgeSize, "younger than %s", CleanAgeDisplay(opts.Age))
	}
	if summary.ActiveCount > 0 {
		row(summary.ActiveSize, "active worktrees (review with clean --guide)")
	}
	if summary.RiskyCount > 0 {
		row(summary.RiskySize, "AI logs (need --risky)")
	}
	if summary.FilterCount > 0 && (len(opts.Categories) > 0 || len(opts.Tools) > 0) {
		row(summary.FilterSize, "outside --category/--tool")
	}
	if summary.AgentStateLiveCount > 0 {
		row(summary.AgentStateLiveSize, "agent state whose project still exists")
	}
	if summary.AgentStateUndeterminedCount > 0 {
		row(summary.AgentStateUndeterminedSize, "agent state not proven orphaned")
	}
	if len(summary.OtherBlocked) > 0 {
		reasons := make([]string, 0, len(summary.OtherBlocked))
		for reason := range summary.OtherBlocked {
			reasons = append(reasons, string(reason))
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			row(summary.OtherBlocked[cleaner.EligibilityReason(reason)].Size, "%s", reason)
		}
	}
}
