package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

func TestScanHeadlinePinsPressureOnTightVolume(t *testing.T) {
	items := scanHeadlineFixture(t)
	paths := scanReclaimPaths(items, scanNextPolicy())
	report := &volume.Report{
		Role:           "home",
		FSType:         "apfs",
		UsedPercent:    92,
		AvailableBytes: 34 * 1024 * 1024 * 1024,
		Band:           volume.BandLow,
	}

	got := scanHeadline(7*1024*1024*1024+42*1024*1024, paths, report)
	for _, want := range []string{
		"7.0 GB found",
		"largest reclaim 7.0 GB",
		"--pressure",
		"92% used",
		"34.0 GB free",
		"tight",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("headline missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "low") {
		t.Errorf("human headline used JSON band word:\n%s", got)
	}
	if strings.Contains(got, "default clean") {
		t.Errorf("headline led with default clean instead of --pressure:\n%s", got)
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

func scanHeadlineFixture(t *testing.T) []types.DebrisInfo {
	t.Helper()
	base := t.TempDir()
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
