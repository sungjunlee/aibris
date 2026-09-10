package cmd

import (
	"fmt"
	"os"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

func currentProtectScanRoots() []string {
	roots, err := scanner.NormalizeRoots(cleanRoots)
	if err != nil {
		return nil
	}
	return roots
}

// newProtectPathMatcher resolves --protect-path flags against approved scan
// roots using the same canonicalization and outside-root rejection as
// --exclude. It is clean-only: scan inventory is not filtered.
func newProtectPathMatcher(roots []string) *exclude.Matcher {
	if len(cleanProtectPaths) == 0 {
		return nil
	}
	patterns := make([]exclude.Pattern, 0, len(cleanProtectPaths))
	for _, raw := range cleanProtectPaths {
		patterns = append(patterns, exclude.Pattern{Raw: raw, Source: types.ExcludeSourceFlag})
	}
	return exclude.New(patterns, roots)
}

// applyProtectPathProtections removes matching items from the selected target
// set and records an explicit protect-path reason so they stay in the plan as
// protected. It never makes a non-candidate selectable.
func applyProtectPathProtections(
	inventory, targets []types.DebrisInfo,
	matcher *exclude.Matcher,
) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	if matcher == nil {
		return targets, nil
	}
	protections := make(map[string]cleanAuditReason)
	protectedPaths := make(map[string]bool)
	for _, item := range inventory {
		if !matcher.ProtectMatch(item.Path) {
			continue
		}
		protections[cleanAuditItemKey(item)] = cleanReasonProtectPath
		if key, ok := cleaner.TargetPathKey(item.Path); ok {
			protectedPaths[key] = true
		}
	}
	if len(protections) == 0 && len(protectedPaths) == 0 {
		return targets, protections
	}
	filtered := make([]types.DebrisInfo, 0, len(targets))
	for _, target := range targets {
		if _, ok := protections[cleanAuditItemKey(target)]; ok {
			continue
		}
		if key, ok := cleaner.TargetPathKey(target.Path); ok && protectedPaths[key] {
			protections[cleanAuditItemKey(target)] = cleanReasonProtectPath
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered, protections
}

func applyProtectPathToGuidedState(state *guidedCleanState, matcher *exclude.Matcher) {
	if state == nil || matcher == nil {
		return
	}
	for i := range state.Rows {
		if !matcher.ProtectMatch(state.Rows[i].Row.Item.Path) {
			continue
		}
		state.Rows[i].Policy = guidedCleanPolicyLocked
		state.Rows[i].Selected = false
		state.Rows[i].SelectionOverride = nil
		state.Rows[i].Row.Reason = string(cleanReasonProtectPath)
		state.Rows[i].ReasonCodes = []DecisionReasonCode{DecisionReasonCode("protect_path")}
	}
}

func protectPathRefusedTargets(targets []types.DebrisInfo, protections map[string]cleanAuditReason) []types.DebrisInfo {
	if len(protections) == 0 {
		return nil
	}
	var refused []types.DebrisInfo
	for _, target := range targets {
		if protections[cleanAuditItemKey(target)] == cleanReasonProtectPath {
			refused = append(refused, target)
		}
	}
	return refused
}

func printProtectPathDiagnostics(matcher *exclude.Matcher) {
	if matcher == nil {
		return
	}
	rejected := matcher.Rejected()
	if len(rejected) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "protect-path (clean only)")
	for _, item := range rejected {
		fmt.Fprintf(os.Stderr, "  rejected  %-11s %s  %s\n", item.Source, item.Pattern, item.Reason)
	}
}

func jsonProtectPathsFromMatcher(matcher *exclude.Matcher) *scanreport.JSONProtectPaths {
	if matcher == nil {
		return nil
	}
	scopes := matcher.Scopes()
	protectedCount := 0
	for _, scope := range scopes {
		protectedCount += scope.Count
	}
	return scanreport.JSONProtectPathsFrom(protectedCount, scopes, matcher.Rejected())
}
