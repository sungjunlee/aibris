package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func confirmCleanExecution() bool {
	fmt.Print("Proceed? [y/N]: ")
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		fmt.Println("No confirmation received; rerun with --dry-run to review or --force to delete selected targets.")
		return false
	}
	if response != "y" && response != "Y" {
		fmt.Println("Aborted.")
		return false
	}
	return true
}

type cleanPlanMode string

const (
	cleanPlanModeDelete cleanPlanMode = "delete"
	cleanPlanModeDryRun cleanPlanMode = "dry-run"
)

func parseAge(s string) (time.Duration, error) {
	units := []struct {
		suffix string
		unit   time.Duration
	}{
		{suffix: "mo", unit: 30 * 24 * time.Hour},
		{suffix: "y", unit: 365 * 24 * time.Hour},
		{suffix: "w", unit: 7 * 24 * time.Hour},
		{suffix: "d", unit: 24 * time.Hour},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0, err
			}
			return time.Duration(n * float64(u.unit)), nil
		}
	}
	return time.ParseDuration(s)
}

func printCleanHeader(roots []string) {
	fmt.Println("clean")
	fmt.Printf("  roots  %s\n\n", strings.Join(displayRoots(roots), ", "))
}

func shortDurationString(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func printCleanCandidateSummary(targets []types.DebrisInfo) {
	var totalSize int64
	for _, w := range targets {
		totalSize += w.Size
	}
	fmt.Printf("  matched  %d %s   %s\n\n",
		len(targets), candidateNoun(len(targets)), cleaner.FormatSize(totalSize))
}

func candidateNoun(count int) string {
	if count == 1 {
		return "candidate"
	}
	return "candidates"
}
