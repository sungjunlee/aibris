// Package scanreport is the cobra-free scan view-model: reclaim paths,
// default-clean projection, volume pressure, and review-only worktrees.
package scanreport

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

const (
	labelDefaultDelete = "default delete"
	labelStrip         = "strip (keep trees)"
	labelPressure      = "pressure caches"
)

// View is the typed scan report printers format. Tests should assert this
// model rather than cobra output snapshots.
type View struct {
	ReclaimPaths     []ReclaimPath
	DefaultCleanSize int64
	DefaultClean     CleanupProjection
	StripEstimate    int64
	PressureEstimate int64
	Volume           *volume.Report
	ReviewOnly       ReviewOnly
}

// ReclaimPath is one operator-facing reclaim command and its estimated size.
type ReclaimPath struct {
	Label   string
	Size    int64
	Command string
}

// ReviewOnly is the review-only worktree count and size. Those units are
// never cleanup or --strip targets.
type ReviewOnly struct {
	Count int
	Size  int64
}

// New projects items through the default-clean policy into the scan view-model.
func New(items []types.DebrisInfo, policy types.PruneOptions) View {
	cleanup := SummarizeCleanup(items, policy)
	return View{
		ReclaimPaths:     ReclaimPaths(items, policy),
		DefaultCleanSize: cleanup.EligibleSize,
		DefaultClean:     cleanup,
		StripEstimate:    StripEstimate(items, policy),
		PressureEstimate: PressureEstimate(items, policy),
		Volume:           HomeVolumeReport(items),
		ReviewOnly:       ReviewOnlyStats(items),
	}
}

// DefaultCleanPolicy is the prune policy scan uses for the default-clean
// estimate. It matches clean's own defaults, including the agent-state idle
// floor and automatic critical-volume cache relaxation.
func DefaultCleanPolicy() types.PruneOptions {
	opts := types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	opts.RelaxCacheAge, opts.PressureDevice = autoRelaxCacheAge()
	return opts
}

func autoRelaxCacheAge() (bool, string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false, ""
	}
	report, err := volume.Inspect(home)
	if err != nil || report.Band != volume.BandCritical {
		return false, ""
	}
	dev, err := volume.PathDevice(home)
	if err != nil {
		return false, ""
	}
	return true, dev
}

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

// LargestNonDefault is the biggest reclaim path that beats default delete.
func LargestNonDefault(paths []ReclaimPath) (ReclaimPath, bool) {
	def := SizeByLabel(paths, labelDefaultDelete)
	var best ReclaimPath
	found := false
	for _, path := range paths {
		if !beatsDefaultReclaim(path, def) {
			continue
		}
		if !found || path.Size > best.Size {
			best = path
			found = true
		}
	}
	return best, found
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

// Flag is the operator-facing clean flag for this reclaim path.
func (p ReclaimPath) Flag() string {
	switch p.Label {
	case labelPressure:
		return "--pressure"
	case labelStrip:
		return "--strip"
	default:
		return "default"
	}
}

func isReviewOnlyWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Status != types.WorktreeActive &&
		item.Status != types.WorktreeOrphaned
}

// ReviewOnlyStats counts worktree units that are not cleanup or --strip targets.
func ReviewOnlyStats(items []types.DebrisInfo) ReviewOnly {
	var stats ReviewOnly
	for _, item := range items {
		if !isReviewOnlyWorktree(item) {
			continue
		}
		stats.Count++
		stats.Size += item.Size
	}
	return stats
}
