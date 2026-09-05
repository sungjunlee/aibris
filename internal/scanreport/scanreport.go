// Package scanreport is the cobra-free scan report: one in-memory View,
// with JSON and human renderers. jsonOutput and friends are encode-only
// projections of View, not a second domain graph.
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

// View is the only in-memory scan report model. JSON and human printers
// render from this type. Tests should assert this model rather than cobra
// output snapshots.
type View struct {
	Partial              bool
	ProviderErrors       []types.ScanProviderError
	Items                []Item
	TotalCount           int
	TotalSize            int64
	PhysicalUnitCount    int
	PhysicalTotalBytes   int64
	TotalStrippableBytes int64
	ByCategory           map[types.Category]types.CategorySummary
	ByTool               map[types.Tool]types.ToolSummary
	Retention            types.RetentionProjection
	Diagnostics          []types.ProviderDiagnostic
	ExcludedByUser       int
	ExcludedScopes       []types.ExcludedScope
	RejectedExcludes     []types.RejectedExclude
	Policy               types.PruneOptions
	CodexActivity        *CodexActivityNotice

	ReclaimPaths     []ReclaimPath
	DefaultCleanSize int64
	DefaultClean     CleanupProjection
	StripEstimate    int64
	PressureEstimate int64
	Volume           *volume.Report
	ReviewOnly       ReviewOnly

	debris []types.DebrisInfo
}

// Item is one scan-report row with derived presentation fields. JSON encoding
// projects this onto the public schema; it is not a second debris graph.
type Item struct {
	Tool             types.Tool
	Category         types.Category
	ID               string
	Project          string
	Source           string
	Path             string
	Size             int64
	ModTime          time.Time
	Status           types.WorktreeStatus
	Classification   types.EntryClass
	Risk             string
	Reason           string
	CleanupKind      types.CleanupKind
	CleanupCommand   []string
	PhysicalTargetID string
	StrippableBytes  int64
	StrippablePaths  []string
	ReviewOnly       bool
}

// CodexActivityNotice is the optional human-only Codex activity line. JSON
// does not serialize it. cmd fills it from the session index.
type CodexActivityNotice struct {
	ProtectedCount int
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
		Items:            projectItems(items),
		Policy:           policy,
		ReclaimPaths:     ReclaimPaths(items, policy),
		DefaultCleanSize: cleanup.EligibleSize,
		DefaultClean:     cleanup,
		StripEstimate:    StripEstimate(items, policy),
		PressureEstimate: PressureEstimate(items, policy),
		Volume:           HomeVolumeReport(items),
		ReviewOnly:       ReviewOnlyStats(items),
		debris:           items,
	}
}

// FromResult projects a ScanResult plus default-clean policy into the single
// in-memory scan report, including the cleanup/reclaim projections the human
// report prints. The JSON encode path uses FromResultJSON instead.
func FromResult(r *types.ScanResult, policy types.PruneOptions) View {
	if r == nil {
		r = &types.ScanResult{}
	}
	view := New(r.Worktrees, policy)
	view.Partial = r.Partial()
	view.ProviderErrors = r.ProviderErrors
	view.TotalCount = r.TotalCount
	view.TotalSize = r.TotalSize
	view.PhysicalUnitCount = r.PhysicalUnitCount
	view.PhysicalTotalBytes = r.PhysicalTotalBytes
	view.TotalStrippableBytes = r.TotalStrippableBytes
	view.ByCategory = r.ByCategory
	view.ByTool = r.ByTool
	view.Retention = r.Retention
	view.Diagnostics = r.Diagnostics
	view.ExcludedByUser = r.ExcludedByUser
	view.ExcludedScopes = r.ExcludedScopes
	view.RejectedExcludes = r.RejectedExcludes
	return view
}

// FromResultJSON projects a ScanResult into the scan view-model for the JSON
// encode path only. It fills exactly the fields EncodeJSON consumes and skips
// the cleanup/reclaim/strip/pressure projections FromResult computes for the
// human report, so scan --json pays only the HomeVolumeReport inspect.
func FromResultJSON(r *types.ScanResult) View {
	if r == nil {
		r = &types.ScanResult{}
	}
	return View{
		Items:                projectItems(r.Worktrees),
		Partial:              r.Partial(),
		ProviderErrors:       r.ProviderErrors,
		TotalCount:           r.TotalCount,
		TotalSize:            r.TotalSize,
		PhysicalUnitCount:    r.PhysicalUnitCount,
		PhysicalTotalBytes:   r.PhysicalTotalBytes,
		TotalStrippableBytes: r.TotalStrippableBytes,
		ByCategory:           r.ByCategory,
		ByTool:               r.ByTool,
		Retention:            r.Retention,
		Diagnostics:          r.Diagnostics,
		ExcludedByUser:       r.ExcludedByUser,
		ExcludedScopes:       r.ExcludedScopes,
		RejectedExcludes:     r.RejectedExcludes,
		Volume:               HomeVolumeReport(r.Worktrees),
		debris:               r.Worktrees,
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
	opts.RelaxCacheAge, opts.PressureDevice = AutoRelaxCacheAge()
	return opts
}

// AutoRelaxCacheAge reports whether default-clean should ignore --age for
// official regenerable caches on the home volume. True only when that volume
// is critical. The returned device limits relaxation to that volume.
func AutoRelaxCacheAge() (bool, string) {
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
	return cleaner.IsReviewOnlyWorktree(item)
}

// ReviewOnlyStats counts worktree units that are not cleanup or --strip targets.
func ReviewOnlyStats(items []types.DebrisInfo) ReviewOnly {
	count, size := cleaner.ReviewOnlyWorktreeStats(items)
	return ReviewOnly{Count: count, Size: size}
}
