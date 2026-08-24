package scanreport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

func testPolicy() types.PruneOptions {
	return types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
}

func TestHeadlinePinsPressureOnTightVolume(t *testing.T) {
	items := headlineFixture(t)
	paths := ReclaimPaths(items, testPolicy())
	report := &volume.Report{
		Role:           "home",
		FSType:         "apfs",
		UsedPercent:    92,
		AvailableBytes: 34 * 1024 * 1024 * 1024,
		Band:           volume.BandLow,
	}

	got := Headline(7*1024*1024*1024+42*1024*1024, paths, report)
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
}

func TestPressureEstimateIgnoresOtherVolumeCaches(t *testing.T) {
	items := homeAndOtherVolumeCaches(t)
	got := PressureEstimate(items, testPolicy())
	want := int64(42*1024*1024 + 7*1024*1024*1024)
	if got != want {
		t.Fatalf("home pressure = %d; want %d (home cache+orphaned, not other-volume)", got, want)
	}

	paths := ReclaimPaths(items, testPolicy())
	report := &volume.Report{
		Role: "home", FSType: "apfs", UsedPercent: 92,
		AvailableBytes: 34 * 1024 * 1024 * 1024, Band: volume.BandLow,
	}
	headline := Headline(7*1024*1024*1024+42*1024*1024, paths, report)
	if !strings.Contains(headline, "--pressure") || !strings.Contains(headline, "tight") {
		t.Fatalf("headline missing home --pressure:\n%s", headline)
	}
	if strings.Contains(headline, "50.0 GB") {
		t.Fatalf("headline used off-volume cache size:\n%s", headline)
	}
	if SizeByLabel(paths, labelPressure) == 50*1024*1024*1024 {
		t.Fatalf("pressure path used off-volume cache size: %+v", paths)
	}
}

func TestReclaimPathsKeepsHomePressureWhenOffVolumeDefaultIsLarger(t *testing.T) {
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
	homeCache := filepath.Join(home, "go-build")
	otherOrphan := filepath.Join(home, "other-vol", "orphaned")
	for _, path := range []string{homeCache, otherOrphan} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	items := []types.DebrisInfo{
		{
			ID: "other-orphan", Tool: types.ToolCodex, Category: types.CategoryWorktree,
			Status: types.WorktreeOrphaned, Path: otherOrphan,
			Size: 10 * 1024 * 1024 * 1024, ModTime: old,
		},
		{
			ID: "home-cache", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
			Path: homeCache, Size: 7 * 1024 * 1024 * 1024, ModTime: recent,
		},
	}

	paths := ReclaimPaths(items, testPolicy())
	got := reclaimPathMap(paths)
	if got["aibris clean --dry-run"] != 10*1024*1024*1024 {
		t.Fatalf("default delete = %d; want global 10 GiB; paths=%v", got["aibris clean --dry-run"], paths)
	}
	if got["aibris clean --pressure --dry-run"] != 7*1024*1024*1024 {
		t.Fatalf("pressure = %d; want home 7 GiB; paths=%v", got["aibris clean --pressure --dry-run"], paths)
	}

	report := &volume.Report{
		Role: "home", FSType: "apfs", UsedPercent: 92,
		AvailableBytes: 34 * 1024 * 1024 * 1024, Band: volume.BandLow,
	}
	found := int64(17 * 1024 * 1024 * 1024)
	headline := Headline(found, paths, report)
	for _, want := range []string{"largest reclaim 7.0 GB", "--pressure", "tight"} {
		if !strings.Contains(headline, want) {
			t.Errorf("headline missing %q:\n%s", want, headline)
		}
	}
}

func TestPressureEstimateSkipsWhenHomeDeviceUnknown(t *testing.T) {
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
	got := PressureEstimate(items, testPolicy())
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

func headlineFixture(t *testing.T) []types.DebrisInfo {
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

func reclaimPathMap(paths []ReclaimPath) map[string]int64 {
	got := make(map[string]int64, len(paths))
	for _, path := range paths {
		got[path.Command] = path.Size
	}
	return got
}
