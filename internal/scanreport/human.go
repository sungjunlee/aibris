package scanreport

import (
	"fmt"
	"io"

	"github.com/sungjunlee/aibris/internal/cleaner"
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
		writeDefaultCacheRelaxNote(w, view.Policy)
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
