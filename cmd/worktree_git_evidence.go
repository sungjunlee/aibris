package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const gitEvidenceCommandTimeout = 5 * time.Second

// GitEvidenceReasonCode is a stable machine-readable recoverability result.
type GitEvidenceReasonCode string

const (
	GitReasonEvidenceUnavailable      GitEvidenceReasonCode = "git_evidence_unavailable"
	GitReasonDirtyWorktree            GitEvidenceReasonCode = "git_dirty_or_untracked"
	GitReasonAttachedBranch           GitEvidenceReasonCode = "git_attached_local_branch"
	GitReasonDetachedHeadReachable    GitEvidenceReasonCode = "git_detached_head_reachable"
	GitReasonDetachedHeadUnreferenced GitEvidenceReasonCode = "git_detached_head_unreferenced"
)

// GitEvidenceReason pairs a stable result code with human-facing context. The
// member path makes aggregate cleanup-unit failures attributable and testable.
type GitEvidenceReason struct {
	Code         GitEvidenceReasonCode
	Description  string
	WorktreePath string
}

// GitUpstreamState is explanatory only and never determines recoverability.
type GitUpstreamState string

const (
	GitUpstreamNotApplicable GitUpstreamState = "not_applicable"
	GitUpstreamNone          GitUpstreamState = "none"
	GitUpstreamPresent       GitUpstreamState = "present"
	GitUpstreamGone          GitUpstreamState = "gone"
	GitUpstreamUnavailable   GitUpstreamState = "unavailable"
)

// GitUpstreamMetadata explains tracking configuration without making it a
// hard-safety input. Ref remains populated for a configured but gone upstream.
type GitUpstreamMetadata struct {
	State GitUpstreamState
	Ref   string
}

func inspectGitWorktreeEvidence(ctx context.Context, member *GitWorktreeMember, runner worktreeGitCommandRunner) {
	status, err := runner(ctx, member.WorktreePath, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("reading porcelain status: %w", err))
		return
	}
	member.Dirty = len(status) > 0

	headOutput, err := runner(ctx, member.WorktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("resolving HEAD: %w", err))
		return
	}
	member.HeadOID, err = gitOID(headOutput)
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("resolving HEAD: %w", err))
		return
	}

	branchOutput, err := runner(ctx, member.WorktreePath, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("resolving attached branch: %w", err))
		return
	}
	branchValue, err := singleGitValue(branchOutput)
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("resolving attached branch: %w", err))
		return
	}
	if branchValue != "HEAD" {
		if !strings.HasPrefix(branchValue, "refs/heads/") {
			markGitEvidenceUnavailable(member, fmt.Errorf("resolving attached branch: unexpected branch ref %q", branchValue))
			return
		}
		member.BranchRef = branchValue
		member.Upstream = inspectGitUpstream(ctx, member.WorktreePath, member.BranchRef, runner)
	} else {
		member.Upstream.State = GitUpstreamNotApplicable
	}

	member.ContainingLocalRefs, err = containingGitRefs(ctx, member.WorktreePath, member.HeadOID, "refs/heads", runner)
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("enumerating containing local refs: %w", err))
		return
	}
	member.ContainingRemoteRefs, err = containingGitRefs(ctx, member.WorktreePath, member.HeadOID, "refs/remotes", runner)
	if err != nil {
		markGitEvidenceUnavailable(member, fmt.Errorf("enumerating containing remote refs: %w", err))
		return
	}

	member.GitEvidenceAvailable = true
	var recoverableCode GitEvidenceReasonCode
	var recoverableDescription string
	switch {
	case member.BranchRef != "":
		member.Recoverable = true
		recoverableCode = GitReasonAttachedBranch
		recoverableDescription = fmt.Sprintf("local branch retained: %s", member.BranchRef)
	case len(member.ContainingLocalRefs) > 0 || len(member.ContainingRemoteRefs) > 0:
		member.Recoverable = true
		recoverableCode = GitReasonDetachedHeadReachable
		recoverableDescription = "detached HEAD reachable from named ref"
	default:
		lockGitMember(member, GitReasonDetachedHeadUnreferenced, "detached HEAD not reachable from named ref")
		return
	}
	if member.Dirty {
		lockGitMember(member, GitReasonDirtyWorktree, "dirty or untracked files")
		return
	}
	markGitMemberRecoverable(member, recoverableCode, recoverableDescription)
}

func containingGitRefs(ctx context.Context, worktreePath, headOID, namespace string, runner worktreeGitCommandRunner) ([]string, error) {
	output, err := runner(ctx, worktreePath, "for-each-ref", "--format=%(refname)", "--contains="+headOID, namespace)
	if err != nil {
		return nil, err
	}
	refs := nonEmptyGitLines(output)
	for _, ref := range refs {
		if !strings.HasPrefix(ref, namespace+"/") {
			return nil, fmt.Errorf("unexpected ref %q outside %s", ref, namespace)
		}
	}
	sort.Strings(refs)
	return refs, nil
}

func inspectGitUpstream(ctx context.Context, worktreePath, branchRef string, runner worktreeGitCommandRunner) GitUpstreamMetadata {
	output, err := runner(ctx, worktreePath, "for-each-ref", "--format=%(upstream)%00%(upstream:track)", branchRef)
	if err != nil {
		return GitUpstreamMetadata{State: GitUpstreamUnavailable}
	}
	line := strings.TrimSuffix(string(output), "\n")
	parts := strings.Split(line, "\x00")
	if len(parts) != 2 {
		return GitUpstreamMetadata{State: GitUpstreamUnavailable}
	}
	upstreamRef := strings.TrimSpace(parts[0])
	tracking := strings.TrimSpace(parts[1])
	if upstreamRef == "" {
		return GitUpstreamMetadata{State: GitUpstreamNone}
	}
	if strings.Contains(tracking, "[gone]") {
		return GitUpstreamMetadata{State: GitUpstreamGone, Ref: upstreamRef}
	}
	return GitUpstreamMetadata{State: GitUpstreamPresent, Ref: upstreamRef}
}

func singleGitValue(output []byte) (string, error) {
	lines := nonEmptyGitLines(output)
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one value, got %d", len(lines))
	}
	return lines[0], nil
}

func gitOID(output []byte) (string, error) {
	value, err := singleGitValue(output)
	if err != nil {
		return "", err
	}
	if len(value) != 40 && len(value) != 64 {
		return "", fmt.Errorf("unexpected object ID length %d", len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid object ID: %w", err)
	}
	return value, nil
}

func nonEmptyGitLines(output []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func markGitEvidenceUnavailable(member *GitWorktreeMember, err error) {
	member.GitEvidenceAvailable = false
	member.GitEvidenceError = err.Error()
	member.Recoverable = false
	lockGitMember(member, GitReasonEvidenceUnavailable, "Git evidence unavailable: "+err.Error())
}

func markGitMemberRecoverable(member *GitWorktreeMember, code GitEvidenceReasonCode, description string) {
	member.Recoverable = true
	member.HardLocked = false
	member.Reason = GitEvidenceReason{Code: code, Description: description, WorktreePath: member.WorktreePath}
}

func lockGitMember(member *GitWorktreeMember, code GitEvidenceReasonCode, description string) {
	member.HardLocked = true
	member.Reason = GitEvidenceReason{Code: code, Description: description, WorktreePath: member.WorktreePath}
}
