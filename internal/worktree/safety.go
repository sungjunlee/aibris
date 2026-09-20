package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

const (
	ProtectionDirtyFiles                    = "dirty files"
	ProtectionUnpushedCommits               = "unpushed commits"
	ProtectionGitStatusUnavailable          = "git status unavailable"
	ProtectionUpstreamComparisonUnavailable = "upstream comparison unavailable"
)

// GitSafety is the fail-closed cleanup protection decision for one active
// worktree path. Cmd maps ProtectionReasons onto audit strings.
type GitSafety struct {
	Protected         bool
	ProtectionReasons []string
}

// InspectActiveCleanupSafety uses the same member discovery and Git
// recoverability evidence as the active-worktree executor. The legacy
// upstream comparison remains as a compatibility fallback for cached fixtures
// that describe a regular repository rather than a linked worktree.
func InspectActiveCleanupSafety(ctx context.Context, candidatePath string) GitSafety {
	units, err := BuildWorktreeCleanupUnits(ctx, []types.DebrisInfo{{
		Category: types.CategoryWorktree,
		Path:     candidatePath,
		// Safety inspector for an already-selected active worktree, not a
		// cleanup selector. Empty Status is fail-closed in unit construction.
		Status: types.WorktreeActive,
	}})
	if err != nil {
		return protectedGitSafety(ProtectionGitStatusUnavailable)
	}
	if len(units) == 0 {
		return InspectGitState(ctx, candidatePath)
	}
	if len(units) != 1 {
		return protectedGitSafety(ProtectionGitStatusUnavailable)
	}

	unit := units[0]
	reasons := make([]string, 0, len(unit.HardLockReasons))
	for _, reason := range unit.HardLockReasons {
		reasons = appendGitProtectionReason(reasons, gitEvidenceProtectionReason(reason))
	}
	return GitSafety{Protected: unit.HardLocked, ProtectionReasons: reasons}
}

func gitEvidenceProtectionReason(reason GitEvidenceReason) string {
	switch reason.Code {
	case GitReasonDirtyWorktree:
		return ProtectionDirtyFiles
	case GitReasonEvidenceUnavailable:
		return ProtectionGitStatusUnavailable
	default:
		return reason.Description
	}
}

func InspectGitState(ctx context.Context, candidatePath string) GitSafety {
	return InspectGitStateWithRunner(ctx, candidatePath, RunGitCommand)
}

func InspectGitStateWithRunner(ctx context.Context, candidatePath string, runner GitCommandRunner) GitSafety {
	worktreeDir, ok := candidateGitWorktreeDir(candidatePath)
	if !ok {
		return protectedGitSafety(ProtectionGitStatusUnavailable)
	}

	status, err := runner(ctx, worktreeDir, "status", "--porcelain=v1", "--branch")
	if err != nil {
		return protectedGitSafety(ProtectionGitStatusUnavailable)
	}

	var reasons []string
	statusInfo, ok := parseGitStatusPorcelainBranch(string(status))
	if !ok {
		return protectedGitSafety(ProtectionGitStatusUnavailable)
	}
	if statusInfo.dirty {
		reasons = appendGitProtectionReason(reasons, ProtectionDirtyFiles)
	}
	if statusInfo.detached {
		reasons = appendGitProtectionReason(reasons, ProtectionUpstreamComparisonUnavailable)
		return GitSafety{Protected: true, ProtectionReasons: reasons}
	}

	upstream, err := runner(ctx, worktreeDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil || strings.TrimSpace(string(upstream)) == "" {
		reasons = appendGitProtectionReason(reasons, ProtectionUpstreamComparisonUnavailable)
		return GitSafety{Protected: true, ProtectionReasons: reasons}
	}

	countOutput, err := runner(ctx, worktreeDir, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		reasons = appendGitProtectionReason(reasons, ProtectionUpstreamComparisonUnavailable)
		return GitSafety{Protected: true, ProtectionReasons: reasons}
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(countOutput)))
	if err != nil {
		reasons = appendGitProtectionReason(reasons, ProtectionUpstreamComparisonUnavailable)
		return GitSafety{Protected: true, ProtectionReasons: reasons}
	}
	if count > 0 {
		reasons = appendGitProtectionReason(reasons, ProtectionUnpushedCommits)
	}

	return GitSafety{Protected: len(reasons) > 0, ProtectionReasons: reasons}
}

func protectedGitSafety(reason string) GitSafety {
	return GitSafety{
		Protected:         true,
		ProtectionReasons: []string{reason},
	}
}

type gitStatusInfo struct {
	dirty    bool
	detached bool
}

func parseGitStatusPorcelainBranch(output string) (gitStatusInfo, bool) {
	var info gitStatusInfo
	sawBranch := false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			sawBranch = true
			if gitStatusBranchDetached(line) {
				info.detached = true
			}
			continue
		}
		info.dirty = true
	}
	return info, sawBranch
}

func gitStatusBranchDetached(line string) bool {
	branch := strings.TrimSpace(strings.TrimPrefix(line, "## "))
	return branch == "HEAD" || strings.HasPrefix(branch, "HEAD ")
}

func appendGitProtectionReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func candidateGitWorktreeDir(candidatePath string) (string, bool) {
	info, err := os.Stat(candidatePath)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if hasGitMetadata(candidatePath) {
		return candidatePath, true
	}

	entries, err := os.ReadDir(candidatePath)
	if err != nil {
		return "", false
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(candidatePath, entry.Name())
		if hasGitMetadata(path) {
			matches = append(matches, path)
		}
	}
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

func hasGitMetadata(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}
