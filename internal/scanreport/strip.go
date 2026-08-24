package scanreport

import (
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// StripEstimate is the strippable-byte total clean --strip would reclaim
// from the current working directory.
func StripEstimate(items []types.DebrisInfo, opts types.PruneOptions) int64 {
	cwd, _ := os.Getwd()
	return stripEstimateForCWD(items, opts, cwd)
}

func stripEstimateForCWD(items []types.DebrisInfo, opts types.PruneOptions, cwd string) int64 {
	targets, _ := selectStripTargets(items, opts, cwd)
	var total int64
	for _, target := range targets {
		total += target.StrippableBytes
	}
	return total
}

func selectStripTargets(items []types.DebrisInfo, opts types.PruneOptions, cwd string) (targets, refusedForCWD []types.DebrisInfo) {
	merged, order := mergeStripEligibleByOwner(items, opts, time.Now())
	for _, key := range order {
		item := merged[key]
		if stripUnitContainsCWD(item, cwd) {
			refusedForCWD = append(refusedForCWD, item)
			continue
		}
		targets = append(targets, item)
	}
	return targets, refusedForCWD
}

func mergeStripEligibleByOwner(items []types.DebrisInfo, opts types.PruneOptions, observedAt time.Time) (map[string]types.DebrisInfo, []string) {
	merged := make(map[string]types.DebrisInfo)
	var order []string
	for _, item := range items {
		deleteEligible, deleteReason := cleaner.EvaluateEligibility(item, opts, observedAt)
		if !cleaner.EvaluateStripEligibility(item, deleteEligible, deleteReason) {
			continue
		}
		key, ok := cleaner.TargetPathKey(item.Path)
		if !ok {
			continue
		}
		if existing, seen := merged[key]; seen {
			merged[key] = mergeStripInventories(existing, item)
			continue
		}
		merged[key] = item
		order = append(order, key)
	}
	return merged, order
}

func mergeStripInventories(base, extra types.DebrisInfo) types.DebrisInfo {
	seen := make(map[string]struct{}, len(base.StrippablePaths))
	for _, path := range base.StrippablePaths {
		if key, ok := cleaner.TargetPathKey(path); ok {
			seen[key] = struct{}{}
		}
	}
	for _, path := range extra.StrippablePaths {
		key, ok := cleaner.TargetPathKey(path)
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		base.StrippablePaths = append(base.StrippablePaths, path)
	}
	base.StrippableBytes += extra.StrippableBytes
	return base
}

func stripUnitContainsCWD(item types.DebrisInfo, cwd string) bool {
	if pathContainsCWD(item.Path, cwd) {
		return true
	}
	for _, subtreePath := range item.StrippablePaths {
		if pathContainsCWD(subtreePath, cwd) {
			return true
		}
	}
	return false
}

func pathContainsCWD(path, cwd string) bool {
	if cwd == "" {
		return false
	}
	parent, ok := cleaner.TargetPathKey(path)
	if !ok {
		return false
	}
	current, ok := cleaner.TargetPathKey(cwd)
	if !ok {
		return false
	}
	return parent == current || cleaner.PathContains(parent, current)
}
