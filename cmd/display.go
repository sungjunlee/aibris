package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

func jsonVolumeFromReport(report volume.Report) *jsonVolume {
	return scanreport.JSONVolumeFromReport(report)
}

func printHomeVolumeLine(report *volume.Report) {
	fmt.Printf("  volume     %s  %s  %.0f%% used   %s free   %s\n",
		report.Role, report.FSType, report.UsedPercent,
		cleaner.FormatSize(int64(report.AvailableBytes)), volume.HumanWord(report.Band))
}

func printExclusionDiagnostics(r *types.ScanResult) {
	scanreport.WriteHumanExclusions(os.Stdout, scanreport.View{
		ExcludedByUser:   r.ExcludedByUser,
		ExcludedScopes:   r.ExcludedScopes,
		RejectedExcludes: r.RejectedExcludes,
	})
}

func printReviewOnlyWorktreeLine(n int, size int64) {
	scanreport.WriteReviewOnlyLine(os.Stdout, n, size)
}

func displayRoots(roots []string) []string {
	out := make([]string, len(roots))
	home, err := os.UserHomeDir()
	if err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(home); resolveErr == nil {
			home = resolved
		}
	}
	for i, root := range roots {
		displayRoot := root
		if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
			displayRoot = resolved
		}
		if err == nil {
			out[i] = displayHomePath(home, displayRoot)
		} else {
			out[i] = displayRoot
		}
	}
	return out
}

func displayHomePath(home, path string) string {
	return scanreport.DisplayHomePath(home, path)
}

func itemReason(w types.DebrisInfo) string {
	return scanreport.ItemReason(w)
}

func itemNoun(count int) string {
	return scanreport.ItemNoun(count)
}

func itemName(item types.DebrisInfo) string {
	return scanreport.ItemName(item)
}

func itemProject(item types.DebrisInfo) string {
	return scanreport.ItemProject(item)
}

func itemAgeAndStatus(item types.DebrisInfo) string {
	return scanreport.ItemAgeAndStatus(item)
}

func cleanupKind(w types.DebrisInfo) types.CleanupKind {
	return scanreport.ItemCleanupKind(w)
}

func cleanAgeDisplay(age time.Duration) string {
	return scanreport.CleanAgeDisplay(age)
}

func ageString(d time.Duration) string {
	return scanreport.AgeString(d)
}
