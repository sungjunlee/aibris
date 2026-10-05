package cmd

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

// MergeGuidedPreviewWithClassicTargets is exported for tests.
func MergeGuidedPreviewWithClassicTargets(guided, classic []types.DebrisInfo) ([]types.DebrisInfo, []types.DebrisInfo) {
	return mergeGuidedPreviewWithClassicTargets(guided, classic)
}

func mergeGuidedPreviewWithClassicTargets(guided, classic []types.DebrisInfo) ([]types.DebrisInfo, []types.DebrisInfo) {
	guidedTargets := cleaner.NormalizeTargets(guided)
	guidedPaths := make([]string, 0, len(guidedTargets))
	for _, target := range guidedTargets {
		if path, ok := cleaner.TargetPathKey(target.Path); ok {
			guidedPaths = append(guidedPaths, path)
		}
	}

	classicTargets := make([]types.DebrisInfo, 0, len(classic))
	for _, target := range classic {
		path, ok := cleaner.TargetPathKey(target.Path)
		if !ok {
			continue
		}
		overlapsGuided := false
		for _, guidedPath := range guidedPaths {
			if path == guidedPath || cleaner.PathContains(guidedPath, path) || cleaner.PathContains(path, guidedPath) {
				overlapsGuided = true
				break
			}
		}
		if !overlapsGuided {
			classicTargets = append(classicTargets, target)
		}
	}
	classicTargets = cleaner.NormalizeTargets(classicTargets)

	auditTargets := make([]types.DebrisInfo, 0, len(guidedTargets)+len(classicTargets))
	auditTargets = append(auditTargets, guidedTargets...)
	auditTargets = append(auditTargets, classicTargets...)
	return classicTargets, auditTargets
}

func applyGuidedCleanDefaults(cmd *cobra.Command, age time.Duration) time.Duration {
	if cleanCategory == "" {
		cleanCategory = string(types.CategoryWorktree)
	}
	// --guide no longer narrows to Codex. Guided review is built on Git
	// evidence, which every tool's worktree carries, so leaving the tool
	// filter empty admits them all; --tool still narrows when asked.
	return guidedCleanAge(cmd, age)
}

func guidedCleanAge(cmd *cobra.Command, age time.Duration) time.Duration {
	if !cmd.Flags().Changed("age") {
		return DefaultMinIdleAge
	}
	return age
}

// shouldRelaxCacheAge reports whether official regenerable caches may ignore
// --age. --pressure always enables it for every official cache. Automatic
// critical-volume mode only relaxes caches on the home volume.
func shouldRelaxCacheAge(explicit bool) (bool, string) {
	if explicit {
		return true, ""
	}
	return scanreport.AutoRelaxCacheAge()
}
