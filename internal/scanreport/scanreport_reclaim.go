package scanreport

import (
	"fmt"
	"os"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

// ReclaimPaths builds the non-zero reclaim ladder for items under policy.
func ReclaimPaths(items []types.DebrisInfo, defaultPolicy types.PruneOptions) []ReclaimPath {
	var paths []ReclaimPath
	defaultSize := eligibleCleanupSize(items, defaultPolicy)
	paths = appendReclaimPath(paths, labelDefaultDelete, defaultSize, "aibris clean --dry-run")
	stripSize := StripEstimate(items, defaultPolicy)
	paths = appendReclaimPath(paths, labelStrip, stripSize, "aibris clean --strip --dry-run")
	pressure := PressureEstimate(items, defaultPolicy)
	if pressure > HomeDefaultCleanSize(items, defaultPolicy, defaultSize) {
		paths = appendReclaimPath(paths, labelPressure, pressure, "aibris clean --pressure --dry-run")
	}
	return paths
}

// HomeDefaultCleanSize is the default-clean eligible size on the home volume,
// or fallback when the home device cannot be resolved.
func HomeDefaultCleanSize(items []types.DebrisInfo, defaultPolicy types.PruneOptions, fallback int64) int64 {
	homeItems, ok := itemsOnHomeVolume(items)
	if !ok {
		return fallback
	}
	return eligibleCleanupSize(homeItems, defaultPolicy)
}

func appendReclaimPath(paths []ReclaimPath, label string, size int64, command string) []ReclaimPath {
	if size <= 0 {
		return paths
	}
	return append(paths, ReclaimPath{Label: label, Size: size, Command: command})
}

var lookupPathDevice = volume.PathDevice

// PressureEstimate is the home-volume reclaim size under relaxed cache age.
// When the home device is unknown the estimate falls back to default-clean
// size for the full item set.
func PressureEstimate(items []types.DebrisInfo, defaultPolicy types.PruneOptions) int64 {
	homeItems, ok := itemsOnHomeVolume(items)
	if !ok {
		return eligibleCleanupSize(items, defaultPolicy)
	}
	pressure := defaultPolicy
	pressure.RelaxCacheAge = true
	pressure.PressureDevice = ""
	return eligibleCleanupSize(homeItems, pressure)
}

func itemsOnHomeVolume(items []types.DebrisInfo) ([]types.DebrisInfo, bool) {
	dev, ok := homePressureDevice()
	if !ok {
		return nil, false
	}
	home := make([]types.DebrisInfo, 0, len(items))
	for _, item := range items {
		if itemOnDevice(item.Path, dev) {
			home = append(home, item)
		}
	}
	return home, true
}

func homePressureDevice() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	dev, err := lookupPathDevice(home)
	if err != nil || dev == "" {
		return "", false
	}
	return dev, true
}

func itemOnDevice(path, device string) bool {
	got, err := lookupPathDevice(path)
	return err == nil && got == device
}

// HomeVolumeReport inspects the home volume and attributes physical debris
// to that device versus others.
func HomeVolumeReport(items []types.DebrisInfo) *volume.Report {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	report, err := volume.Inspect(home)
	if err != nil {
		return nil
	}
	report.Role = "home"
	dev, err := volume.PathDevice(home)
	if err != nil {
		dev = ""
	}
	on, other := volume.SplitDebris(dev, cleaner.PhysicalInventory(items))
	report.DebrisBytes = on
	report.OtherVolumeDebrisBytes = other
	return &report
}

// Headline is the one-line scan summary: found size, largest non-default
// reclaim, and home-volume pressure.
func Headline(found int64, paths []ReclaimPath, report *volume.Report) string {
	parts := []string{fmt.Sprintf("%s found", cleaner.FormatSize(found))}
	if path, ok := LargestNonDefault(paths); ok {
		parts = append(parts, fmt.Sprintf("largest reclaim %s (%s)",
			cleaner.FormatSize(path.Size), path.Flag()))
	}
	if report != nil {
		parts = append(parts, fmt.Sprintf("%.0f%% used   %s free   %s",
			report.UsedPercent,
			cleaner.FormatSize(int64(report.AvailableBytes)),
			volume.HumanWord(report.Band)))
	}
	return "  " + strings.Join(parts, "   ")
}

func beatsDefaultReclaim(path ReclaimPath, defaultSize int64) bool {
	switch path.Label {
	case labelDefaultDelete:
		return false
	case labelPressure:
		return true
	default:
		return path.Size > defaultSize
	}
}

// SizeByLabel returns the estimated size of the named reclaim path, or 0.
func SizeByLabel(paths []ReclaimPath, label string) int64 {
	for _, path := range paths {
		if path.Label == label {
			return path.Size
		}
	}
	return 0
}
