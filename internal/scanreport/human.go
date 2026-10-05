package scanreport

import (
	"fmt"
	"io"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

// summaryLabelWidth aligns the summary's label column. Continuation lines of
// a group indent to the same column.
const summaryLabelWidth = 12

// summaryRow prints one summary line: a fixed-width label, then the text.
// An empty label continues the previous group.
func summaryRow(w io.Writer, label, format string, args ...any) {
	fmt.Fprintf(w, "  %-*s %s\n", summaryLabelWidth, label, fmt.Sprintf(format, args...))
}

// WriteHuman renders the human scan report from View.
func WriteHuman(w io.Writer, view View) {
	fmt.Fprintln(w, "summary")
	if view.Partial {
		summaryRow(w, "incomplete", "results are partial; cleanup is disabled for this scan")
		for _, providerErr := range view.ProviderErrors {
			summaryRow(w, "", "%s failed: %s", providerErr.Tool, providerErr.Message)
		}
	}
	summaryRow(w, "found", "%s in %d %s",
		cleaner.FormatSize(view.PhysicalTotalBytes), view.TotalCount, ItemNoun(view.TotalCount))
	if view.Partial {
		summaryRow(w, "reclaimable", "unknown until a complete scan succeeds")
	} else {
		summaryRow(w, "reclaimable", "%s by default (estimate)", cleaner.FormatSize(view.DefaultCleanSize))
		writeCacheRelaxNote(w, view.Policy)
		if path, ok := LargestNonDefault(view.ReclaimPaths); ok {
			summaryRow(w, "", "%s with %s", cleaner.FormatSize(path.Size), path.Flag())
		}
	}
	if view.TotalStrippableBytes > 0 {
		summaryRow(w, "strippable", "%s of dependencies and build output inside kept worktrees",
			cleaner.FormatSize(view.TotalStrippableBytes))
	}
	if !view.Partial {
		WriteCleanupDiagnostics(w, view.DefaultClean, view.Policy)
	}
	WriteVolumePressure(w, view.Volume)

	WriteHumanExclusions(w, view)
	writeCategorySummary(w, view.ByCategory)
	writeLargestItems(w, view.Items)
	WriteRetention(w, view.Retention)
	writeCodexActivity(w, view.CodexActivity)
	writeDiagnostics(w, view.Diagnostics)
	WriteNext(w, view)
}
