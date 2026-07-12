package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

const (
	gitProtectionDirtyFiles                    = "dirty files"
	gitProtectionUnpushedCommits               = "unpushed commits"
	gitProtectionGitStatusUnavailable          = "git status unavailable"
	gitProtectionUpstreamComparisonUnavailable = "upstream comparison unavailable"
)

type worktreeGitSafety struct {
	Protected         bool
	ProtectionReasons []string
}

type worktreeGitCommandRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

// inspectActiveWorktreeCleanupSafety uses the same member discovery and Git
// recoverability evidence as the active-worktree executor. The legacy
// upstream comparison remains as a compatibility fallback for cached fixtures
// that describe a regular repository rather than a linked worktree.
func inspectActiveWorktreeCleanupSafety(ctx context.Context, candidatePath string) worktreeGitSafety {
	units, err := buildWorktreeCleanupUnits(ctx, []types.DebrisInfo{{
		Category: types.CategoryWorktree,
		Path:     candidatePath,
	}})
	if err != nil {
		return protectedWorktreeGitSafety(gitProtectionGitStatusUnavailable)
	}
	if len(units) == 0 {
		return inspectWorktreeGitState(ctx, candidatePath)
	}
	if len(units) != 1 {
		return protectedWorktreeGitSafety(gitProtectionGitStatusUnavailable)
	}

	unit := units[0]
	reasons := make([]string, 0, len(unit.HardLockReasons))
	for _, reason := range unit.HardLockReasons {
		reasons = appendGitProtectionReason(reasons, gitEvidenceProtectionReason(reason))
	}
	return worktreeGitSafety{Protected: unit.HardLocked, ProtectionReasons: reasons}
}

func gitEvidenceProtectionReason(reason GitEvidenceReason) string {
	switch reason.Code {
	case GitReasonDirtyWorktree:
		return gitProtectionDirtyFiles
	case GitReasonEvidenceUnavailable:
		return gitProtectionGitStatusUnavailable
	default:
		return reason.Description
	}
}

func inspectWorktreeGitState(ctx context.Context, candidatePath string) worktreeGitSafety {
	return inspectWorktreeGitStateWithRunner(ctx, candidatePath, runWorktreeGitCommand)
}

func inspectWorktreeGitStateWithRunner(ctx context.Context, candidatePath string, runner worktreeGitCommandRunner) worktreeGitSafety {
	worktreeDir, ok := candidateGitWorktreeDir(candidatePath)
	if !ok {
		return protectedWorktreeGitSafety(gitProtectionGitStatusUnavailable)
	}

	status, err := runner(ctx, worktreeDir, "status", "--porcelain=v1", "--branch")
	if err != nil {
		return protectedWorktreeGitSafety(gitProtectionGitStatusUnavailable)
	}

	var reasons []string
	statusInfo, ok := parseGitStatusPorcelainBranch(string(status))
	if !ok {
		return protectedWorktreeGitSafety(gitProtectionGitStatusUnavailable)
	}
	if statusInfo.dirty {
		reasons = appendGitProtectionReason(reasons, gitProtectionDirtyFiles)
	}
	if statusInfo.detached {
		reasons = appendGitProtectionReason(reasons, gitProtectionUpstreamComparisonUnavailable)
		return worktreeGitSafety{Protected: true, ProtectionReasons: reasons}
	}

	upstream, err := runner(ctx, worktreeDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil || strings.TrimSpace(string(upstream)) == "" {
		reasons = appendGitProtectionReason(reasons, gitProtectionUpstreamComparisonUnavailable)
		return worktreeGitSafety{Protected: true, ProtectionReasons: reasons}
	}

	countOutput, err := runner(ctx, worktreeDir, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		reasons = appendGitProtectionReason(reasons, gitProtectionUpstreamComparisonUnavailable)
		return worktreeGitSafety{Protected: true, ProtectionReasons: reasons}
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(countOutput)))
	if err != nil {
		reasons = appendGitProtectionReason(reasons, gitProtectionUpstreamComparisonUnavailable)
		return worktreeGitSafety{Protected: true, ProtectionReasons: reasons}
	}
	if count > 0 {
		reasons = appendGitProtectionReason(reasons, gitProtectionUnpushedCommits)
	}

	return worktreeGitSafety{Protected: len(reasons) > 0, ProtectionReasons: reasons}
}

func runWorktreeGitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	return cmd.CombinedOutput()
}

func protectedWorktreeGitSafety(reason string) worktreeGitSafety {
	return worktreeGitSafety{
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
