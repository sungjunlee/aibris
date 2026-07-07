package cmd

import (
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

const (
	guidedCodexDefaultAge     = 24 * time.Hour
	guidedCodexDefaultMinSize = 256 * 1024 * 1024

	guidedCodexReasonZeroSessions                = "zero matching Codex sessions"
	guidedCodexReasonNewerProjectActivity        = "newer Codex activity in same project"
	guidedCodexProtectionCurrentWorkingDirectory = "current working directory"
	guidedCodexProtectionNewestProjectWorktree   = "newest project worktree"
	guidedCodexProtectionLatestSessionToday      = "latest session today"
	guidedCodexProtectionRecentActivity          = "recent Codex activity"
	guidedCodexProtectionBelowSizeThreshold      = "below size threshold"
	guidedCodexProtectionOutsideCodexWorktrees   = "outside Codex worktrees"
	guidedCodexProtectionYoungerThanGuideAge     = "younger than guide age"
	guidedCodexProtectionNoLowRiskSignal         = "no low-risk Codex signal"
)

type guidedCodexWorktreePlanInput struct {
	Worktrees               []types.DebrisInfo
	Activity                codexActivityIndex
	GitSafety               map[string]worktreeGitSafety
	CurrentWorkingDirectory string
	Now                     time.Time
	Age                     time.Duration
	MinSize                 int64
}

type guidedCodexWorktreePlan struct {
	Selected       []guidedCodexWorktreeRow
	Protected      []guidedCodexWorktreeRow
	SelectedCount  int
	SelectedSize   int64
	ProtectedCount int
	ProtectedSize  int64
}

type guidedCodexWorktreeRow struct {
	Item   types.DebrisInfo
	Reason string
}

func buildGuidedCodexWorktreePlan(input guidedCodexWorktreePlanInput) guidedCodexWorktreePlan {
	input = fillGuidedCodexWorktreePlanInput(input)
	candidates := dedupeGuidedCodexWorktrees(input.Worktrees)
	newestByProject := newestGuidedCodexWorktreeByProject(candidates)

	var plan guidedCodexWorktreePlan
	for _, item := range candidates {
		selected, reason := evaluateGuidedCodexWorktree(input, item, newestByProject)
		row := guidedCodexWorktreeRow{Item: item, Reason: reason}
		if selected {
			plan.Selected = append(plan.Selected, row)
			plan.SelectedCount++
			plan.SelectedSize += item.Size
			continue
		}
		plan.Protected = append(plan.Protected, row)
		plan.ProtectedCount++
		plan.ProtectedSize += item.Size
	}

	sortGuidedCodexWorktreeRows(plan.Selected)
	sortGuidedCodexWorktreeRows(plan.Protected)
	return plan
}

func fillGuidedCodexWorktreePlanInput(input guidedCodexWorktreePlanInput) guidedCodexWorktreePlanInput {
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	if input.Age <= 0 {
		input.Age = guidedCodexDefaultAge
	}
	if input.MinSize <= 0 {
		input.MinSize = guidedCodexDefaultMinSize
	}
	return input
}

func dedupeGuidedCodexWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	seen := make(map[string]bool, len(items))
	var candidates []types.DebrisInfo
	for _, item := range items {
		if !isActiveCodexWorktree(item) {
			continue
		}
		path, ok := cleanTargetPathKey(item.Path)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		candidates = append(candidates, item)
	}
	return candidates
}

func newestGuidedCodexWorktreeByProject(items []types.DebrisInfo) map[string]string {
	type newestWorktree struct {
		item  types.DebrisInfo
		count int
	}
	newest := make(map[string]newestWorktree)
	for _, item := range items {
		if item.Project == "" {
			continue
		}
		current := newest[item.Project]
		current.count++
		if current.item.Path == "" ||
			item.ModTime.After(current.item.ModTime) ||
			(item.ModTime.Equal(current.item.ModTime) && guidedCodexStableItemKey(item) < guidedCodexStableItemKey(current.item)) {
			current.item = item
		}
		newest[item.Project] = current
	}

	paths := make(map[string]string, len(newest))
	for project, current := range newest {
		if current.count > 1 {
			paths[project] = current.item.Path
		}
	}
	return paths
}

func evaluateGuidedCodexWorktree(input guidedCodexWorktreePlanInput, item types.DebrisInfo, newestByProject map[string]string) (bool, string) {
	if !isGuidedCodexWorktreePath(item.Path) {
		return false, guidedCodexProtectionOutsideCodexWorktrees
	}
	if guidedCodexWorktreeContainsCWD(item.Path, input.CurrentWorkingDirectory) {
		return false, guidedCodexProtectionCurrentWorkingDirectory
	}
	if !input.Activity.Available {
		return false, codexActivityProtectionUnavailable
	}
	safety, ok := guidedCodexGitSafetyFor(input, item)
	if !ok {
		return false, gitProtectionGitStatusUnavailable
	}
	if safety.Protected || len(safety.ProtectionReasons) > 0 {
		return false, guidedCodexGitProtectionReason(safety)
	}
	if newestByProject[item.Project] == item.Path {
		return false, guidedCodexProtectionNewestProjectWorktree
	}
	if item.ModTime.After(input.Now.Add(-input.Age)) {
		return false, guidedCodexProtectionYoungerThanGuideAge
	}
	if item.Size < input.MinSize {
		return false, guidedCodexProtectionBelowSizeThreshold
	}

	activity := input.Activity.Worktrees[item.ID]
	if activity.SessionCount == 0 {
		return true, guidedCodexReasonZeroSessions
	}
	if guidedCodexActivityIsRecent(activity.LatestSession, input.Now, input.Age) {
		if guidedCodexSameDay(activity.LatestSession, input.Now) {
			return false, guidedCodexProtectionLatestSessionToday
		}
		return false, guidedCodexProtectionRecentActivity
	}
	if input.Activity.ProjectHasSessionAfter(item.Project, activity.LatestSession) {
		return true, guidedCodexReasonNewerProjectActivity
	}
	return false, guidedCodexProtectionNoLowRiskSignal
}

func isGuidedCodexWorktreePath(path string) bool {
	parts := pathParts(path)
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == ".codex" && isCodexActivityWorktreeRoot(parts[i+1]) {
			return true
		}
	}
	return false
}

func guidedCodexWorktreeContainsCWD(worktreePath, cwd string) bool {
	if cwd == "" {
		return false
	}
	worktree, ok := cleanTargetPathKey(worktreePath)
	if !ok {
		return false
	}
	current, ok := cleanTargetPathKey(cwd)
	if !ok {
		return false
	}
	return worktree == current || cleanTargetContains(worktree, current)
}

func guidedCodexGitSafetyFor(input guidedCodexWorktreePlanInput, item types.DebrisInfo) (worktreeGitSafety, bool) {
	if input.GitSafety == nil {
		return worktreeGitSafety{}, false
	}
	if safety, ok := input.GitSafety[item.Path]; ok {
		return safety, true
	}
	path, ok := cleanTargetPathKey(item.Path)
	if !ok {
		return worktreeGitSafety{}, false
	}
	safety, ok := input.GitSafety[path]
	return safety, ok
}

func guidedCodexGitProtectionReason(safety worktreeGitSafety) string {
	if len(safety.ProtectionReasons) == 0 {
		return gitProtectionGitStatusUnavailable
	}
	return strings.Join(safety.ProtectionReasons, ", ")
}

func guidedCodexActivityIsRecent(ts, now time.Time, age time.Duration) bool {
	if ts.IsZero() {
		return false
	}
	return ts.After(now.Add(-age))
}

func guidedCodexSameDay(left, right time.Time) bool {
	left = left.In(right.Location())
	ly, lm, ld := left.Date()
	ry, rm, rd := right.Date()
	return ly == ry && lm == rm && ld == rd
}

func sortGuidedCodexWorktreeRows(rows []guidedCodexWorktreeRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i].Item
		right := rows[j].Item
		if left.Size == right.Size {
			return guidedCodexStableItemKey(left) < guidedCodexStableItemKey(right)
		}
		return left.Size > right.Size
	})
}

func guidedCodexStableItemKey(item types.DebrisInfo) string {
	return strings.Join([]string{item.Project, item.ID, item.Path}, "\x00")
}
