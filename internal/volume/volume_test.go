package volume

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestClassifyBand(t *testing.T) {
	tests := []struct {
		pct  float64
		want Band
	}{
		{0, BandOK},
		{84.9, BandOK},
		{85, BandLow},
		{94.9, BandLow},
		{95, BandCritical},
		{100, BandCritical},
	}
	for _, tt := range tests {
		if got := ClassifyBand(tt.pct); got != tt.want {
			t.Errorf("ClassifyBand(%v) = %q; want %q", tt.pct, got, tt.want)
		}
	}
}

func TestSplitDebrisKeepsUnknownOnVolume(t *testing.T) {
	items := []types.DebrisInfo{
		{Path: filepath.Join(t.TempDir(), "missing-a"), Size: 100},
		{Path: "/no/such/volume-path", Size: 50},
	}
	on, other := SplitDebris("device:1", items)
	if on != 150 || other != 0 {
		t.Fatalf("SplitDebris = %d/%d; want 150 on-volume when lookup fails", on, other)
	}
}

func TestSplitDebrisSeparatesOtherDevice(t *testing.T) {
	dir := t.TempDir()
	dev, err := pathDevice(dir)
	if err != nil {
		t.Skip("pathDevice unavailable:", err)
	}
	items := []types.DebrisInfo{
		{Path: dir, Size: 80},
		{Path: dir, Size: 20},
	}
	on, other := SplitDebris(dev, items)
	if on != 100 || other != 0 {
		t.Fatalf("same-volume split = %d/%d; want 100/0", on, other)
	}
	on, other = SplitDebris("device:other", items)
	if on != 0 || other != 100 {
		t.Fatalf("other-volume split = %d/%d; want 0/100", on, other)
	}
}

func TestInspectHomeHasNoPathInID(t *testing.T) {
	dir := t.TempDir()
	report, err := Inspect(dir)
	if err != nil {
		t.Skip("Inspect unavailable:", err)
	}
	if report.ID == "" || report.FSType == "" {
		t.Fatalf("incomplete report: %+v", report)
	}
	if strings.Contains(report.ID, "/") || strings.Contains(report.ID, dir) {
		t.Fatalf("volume id leaked a path: %q", report.ID)
	}
	if report.TotalBytes == 0 || report.Band == "" {
		t.Fatalf("missing capacity fields: %+v", report)
	}
}
