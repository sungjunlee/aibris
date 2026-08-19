package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
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

func TestScanPressureEstimateIgnoresOtherVolumeCaches(t *testing.T) {
	items := homeAndOtherVolumeCaches(t)
	got := scanPressureEstimate(items, scanNextPolicy())
	want := int64(42*1024*1024 + 7*1024*1024*1024)
	if got != want {
		t.Fatalf("home pressure = %d; want %d (home cache+orphaned, not other-volume)", got, want)
	}

	paths := scanReclaimPaths(items, scanNextPolicy())
	report := &volume.Report{
		Role: "home", FSType: "apfs", UsedPercent: 92,
		AvailableBytes: 34 * 1024 * 1024 * 1024, Band: volume.BandLow,
	}
	headline := scanHeadline(7*1024*1024*1024+42*1024*1024, paths, report)
	if !strings.Contains(headline, "--pressure") || !strings.Contains(headline, "tight") {
		t.Fatalf("headline missing home --pressure:\n%s", headline)
	}
	if strings.Contains(headline, "50.0 GB") {
		t.Fatalf("headline used off-volume cache size:\n%s", headline)
	}
	output := captureOutput(func() {
		printScanHeadlinePaths(7*1024*1024*1024+42*1024*1024, paths, report)
	})
	if strings.Contains(output, "50.0 GB") {
		t.Fatalf("pressure hint used off-volume cache size:\n%s", output)
	}
}

func TestScanPressureEstimateSkipsWhenHomeDeviceUnknown(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	prev := lookupPathDevice
	lookupPathDevice = func(string) (string, error) {
		return "", errors.New("unavailable")
	}
	t.Cleanup(func() { lookupPathDevice = prev })

	cache := filepath.Join(home, "go-build")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	items := []types.DebrisInfo{{
		ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
		Path: cache, Size: 7 * 1024 * 1024 * 1024, ModTime: time.Now().Add(-time.Hour),
	}}
	got := scanPressureEstimate(items, scanNextPolicy())
	if got != 0 {
		t.Fatalf("unknown home device advertised pressure %d; want 0", got)
	}
}

func homeAndOtherVolumeCaches(t *testing.T) []types.DebrisInfo {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	prev := lookupPathDevice
	lookupPathDevice = func(path string) (string, error) {
		if strings.Contains(path, "other-vol") {
			return "device:other", nil
		}
		return "device:home", nil
	}
	t.Cleanup(func() { lookupPathDevice = prev })

	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	orphaned := filepath.Join(home, "orphaned")
	homeCache := filepath.Join(home, "go-build")
	otherCache := filepath.Join(home, "other-vol", "go-build")
	for _, path := range []string{orphaned, homeCache, otherCache} {
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
			ID: "home-cache", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
			Path: homeCache, Size: 7 * 1024 * 1024 * 1024, ModTime: recent,
		},
		{
			ID: "other-cache", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
			Path: otherCache, Size: 50 * 1024 * 1024 * 1024, ModTime: recent,
		},
	}
}

func scanHeadlineFixture(t *testing.T) []types.DebrisInfo {
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
