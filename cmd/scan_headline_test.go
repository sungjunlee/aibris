package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

func TestPrintScanHeadlinePinsPressureOnTightVolume(t *testing.T) {
	items := printHeadlineFixture(t)
	paths := scanreport.ReclaimPaths(items, printScanPolicy())
	report := &volume.Report{
		Role:           "home",
		FSType:         "apfs",
		UsedPercent:    92,
		AvailableBytes: 34 * 1024 * 1024 * 1024,
		Band:           volume.BandLow,
	}

	output := captureOutput(func() {
		printScanHeadlinePaths(7*1024*1024*1024, paths, report)
	})
	if !strings.Contains(output, "--pressure") || !strings.Contains(output, "tight") {
		t.Fatalf("printed headline missing --pressure on tight volume:\n%s", output)
	}
}

func TestJSONVolumeBandStaysLow(t *testing.T) {
	report := volume.Report{Band: volume.BandLow, Role: "home"}
	got := jsonVolumeFromReport(report)
	if got.Band != "low" {
		t.Fatalf("JSON band = %q; want low", got.Band)
	}
	if volume.HumanWord(volume.BandLow) != "tight" {
		t.Fatalf("human word for low = %q; want tight", volume.HumanWord(volume.BandLow))
	}
}

func printScanPolicy() types.PruneOptions {
	return types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
}

func printHeadlineFixture(t *testing.T) []types.DebrisInfo {
	t.Helper()
	base := t.TempDir()
	testutil.SetHome(t, base)
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	orphaned := filepath.Join(base, "orphaned")
	cache := filepath.Join(base, "go-build")
	for _, path := range []string{orphaned, cache} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return []types.DebrisInfo{
		{
			ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
			Status: types.WorktreeOrphaned, Path: orphaned, Size: 42 * 1024 * 1024, ModTime: old,
		},
		{
			ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
			Path: cache, Size: 7 * 1024 * 1024 * 1024, ModTime: recent,
		},
	}
}
