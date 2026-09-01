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
	if count == 1 {
		return "item"
	}
	return "items"
}

func itemName(item types.DebrisInfo) string {
	if item.Category == types.CategoryWorktree && item.Tool == types.ToolUnknown && item.Source != "" {
		return item.Source + "/" + item.ID
	}
	if item.ID != "" {
		return item.ID
	}
	return string(item.Tool)
}

func itemProject(item types.DebrisInfo) string {
	if item.Project != "" {
		return item.Project
	}
	switch item.Category {
	case types.CategoryBuildCache, types.CategoryOtherCache, types.CategoryAILogs:
		return "global"
	default:
		return "-"
	}
}

func itemAgeAndStatus(item types.DebrisInfo) string {
	age := ageString(time.Since(item.ModTime).Round(time.Hour))
	if item.Status == "" {
		return age
	}
	return fmt.Sprintf("%s %s", item.Status, age)
}

func cleanupKind(w types.DebrisInfo) types.CleanupKind {
	if w.CleanupKind != "" {
		return w.CleanupKind
	}
	return types.CleanupRemovePath
}

func cleanAgeDisplay(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}

func ageString(d time.Duration) string {
	if d.Hours() < 24 {
		return "today"
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}
