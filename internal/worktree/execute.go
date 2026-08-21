package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// WorktreeRemover deletes one Git worktree member by repository identity.
type WorktreeRemover func(ctx context.Context, repositoryID, worktreePath string) error

// ExecutionOptions supplies mutation dependencies and optional barriers for
// ExecuteActiveWorktreeUnit. Nil function fields use the production defaults.
// BeforeMutation and AfterMember stay optional so cmd can keep overlap-safety,
// snapshots, receipts, and stdout on its side of the execute seam.
type ExecutionOptions struct {
	RemoveWorktree WorktreeRemover
	RemoveAll      func(string) error
	Getwd          func() (string, error)
	UserHomeDir    func() (string, error)
	// BeforeMutation runs after preflight and again immediately before every
	// physical mutation. A non-nil error is fail-closed: no mutation is
	// attempted for that step.
	BeforeMutation func(context.Context) error
	// AfterMember runs after a member is verified removed. remaining is the
	// number of members not yet processed.
	AfterMember    func(ctx context.Context, remaining int) error
	RemovingMember func(index, total int, path string)
	RemovedMember  func(path string)
}

// MemberExecution records whether one Git worktree member was physically
// removed. It is the package-level counterpart of cmd's member receipt and
// must not grow JSON tags.
type MemberExecution struct {
	WorktreePath string
	Removed      bool
	Error        string
}

// UnitExecution is the physical outcome of executing one cleanup unit. Cmd
// maps this onto receipts and stdout; it is not a user-facing schema.
type UnitExecution struct {
	Members           []MemberExecution
	PhysicalRemoved   bool
	MutationAttempted bool
	// StartedMembers is true once member mutation (including its pre-mutation
	// barrier) has begun. Cmd uses it to decide whether a failure is partial.
	StartedMembers bool
}

// DefaultExecutionOptions returns production mutation dependencies.
func DefaultExecutionOptions() ExecutionOptions {
	return ExecutionOptions{
		RemoveWorktree: RemoveGitWorktree,
		RemoveAll:      os.RemoveAll,
		Getwd:          os.Getwd,
		UserHomeDir:    os.UserHomeDir,
	}
}

func (opts ExecutionOptions) withDefaults() ExecutionOptions {
	if opts.RemoveWorktree == nil {
		opts.RemoveWorktree = RemoveGitWorktree
	}
	if opts.RemoveAll == nil {
		opts.RemoveAll = os.RemoveAll
	}
	if opts.Getwd == nil {
		opts.Getwd = os.Getwd
	}
	if opts.UserHomeDir == nil {
		opts.UserHomeDir = os.UserHomeDir
	}
	return opts
}

// ExecuteActiveWorktreeUnit physically removes one active worktree cleanup
// unit. Dirty, unrecoverable, and evidence-unavailable members fail closed
// during preflight. plain-dir / empty / unknown statuses are never mutated.
func ExecuteActiveWorktreeUnit(
	ctx context.Context,
	target types.DebrisInfo,
	selected WorktreeCleanupUnit,
	opts ExecutionOptions,
) (UnitExecution, error) {
	opts = opts.withDefaults()
	result := UnitExecution{Members: memberExecutions(selected.Members)}

	refreshed, memberErrors, err := PreflightActiveWorktreeUnit(ctx, target, selected, opts)
	if err != nil {
		result.Members = applyMemberErrors(result.Members, memberErrors)
		return result, err
	}
	result.Members = memberExecutions(refreshed.Members)

	if err := callBeforeMutation(ctx, opts); err != nil {
		return result, err
	}

	for i, member := range refreshed.Members {
		result.StartedMembers = true
		if err := callBeforeMutation(ctx, opts); err != nil {
			setUnitPhysicalState(&result, selected)
			return result, err
		}
		if err := ctx.Err(); err != nil {
			setUnitPhysicalState(&result, selected)
			return result, err
		}
		if opts.RemovingMember != nil {
			opts.RemovingMember(i, len(refreshed.Members), member.WorktreePath)
		}
		result.MutationAttempted = true
		if err := opts.RemoveWorktree(ctx, member.RepositoryID, member.WorktreePath); err != nil {
			removed, verificationErr := VerifyRemovedWorktreeMember(ctx, member)
			result.Members[i].Removed = removed
			result.Members[i].Error = err.Error()
			if verificationErr != nil {
				result.Members[i].Error += "; state verification: " + verificationErr.Error()
			}
			setUnitPhysicalState(&result, selected)
			return result, fmt.Errorf("removing active worktree member %q: %s", member.WorktreePath, result.Members[i].Error)
		}

		removed, postconditionErr := VerifyRemovedWorktreeMember(ctx, member)
		result.Members[i].Removed = removed
		if postconditionErr != nil {
			result.Members[i].Error = postconditionErr.Error()
			setUnitPhysicalState(&result, selected)
			return result, fmt.Errorf("verifying active worktree member %q: %v", member.WorktreePath, postconditionErr)
		}
		if opts.AfterMember != nil {
			if err := opts.AfterMember(ctx, len(refreshed.Members)-(i+1)); err != nil {
				setUnitPhysicalState(&result, selected)
				return result, err
			}
		}
		if opts.RemovedMember != nil {
			opts.RemovedMember(member.WorktreePath)
		}
	}

	if !pathDoesNotExist(selected.TargetPath) {
		result.StartedMembers = true
		if err := callBeforeMutation(ctx, opts); err != nil {
			setUnitPhysicalState(&result, selected)
			return result, err
		}
		if err := ctx.Err(); err != nil {
			setUnitPhysicalState(&result, selected)
			return result, err
		}
		result.MutationAttempted = true
		if err := opts.RemoveAll(selected.TargetPath); err != nil {
			setUnitPhysicalState(&result, selected)
			return result, fmt.Errorf("removing cleanup unit container %q: %v", selected.TargetPath, err)
		}
	}
	if !pathDoesNotExist(selected.TargetPath) {
		setUnitPhysicalState(&result, selected)
		return result, fmt.Errorf("cleanup unit target still exists after removal: %q", selected.TargetPath)
	}

	result.PhysicalRemoved = true
	return result, nil
}

// PreflightActiveWorktreeUnit refreshes Git evidence immediately before
// mutation and fail-closes on identity drift, dirty/unrecoverable members,
// missing evidence, CWD containment, or unsafe paths. plain-dir and other
// non-active statuses never pass.
func PreflightActiveWorktreeUnit(
	ctx context.Context,
	target types.DebrisInfo,
	selected WorktreeCleanupUnit,
	opts ExecutionOptions,
) (WorktreeCleanupUnit, map[string]string, error) {
	opts = opts.withDefaults()
	memberErrors := make(map[string]string)
	if err := ctx.Err(); err != nil {
		return WorktreeCleanupUnit{}, memberErrors, err
	}
	if !IsActiveWorktreeTarget(target) {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf(
			"refusing to mutate worktree target %q with status %q",
			target.Path, target.Status,
		)
	}
	home, err := opts.UserHomeDir()
	if err != nil {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("getting home dir: %w", err)
	}
	if !cleaner.IsSafeTarget(home, target) {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("unsafe active worktree path %q rejected", target.Path)
	}
	cwd, err := opts.Getwd()
	if err != nil {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("getting current working directory: %w", err)
	}
	if cleanupUnitContainsPath(selected.TargetPath, cwd) {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("current working directory is inside cleanup unit %q", selected.TargetPath)
	}

	units, err := BuildWorktreeCleanupUnits(ctx, []types.DebrisInfo{target})
	if err != nil {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("refreshing active worktree evidence: %w", err)
	}
	if len(units) != 1 {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("refreshing active worktree evidence: expected one cleanup unit, found %d", len(units))
	}
	refreshed := units[0]
	if refreshed.TargetPath != selected.TargetPath {
		return refreshed, memberErrors, fmt.Errorf("cleanup unit path changed from %q to %q", selected.TargetPath, refreshed.TargetPath)
	}

	selectedMembers := membersByPath(selected.Members)
	refreshedMembers := membersByPath(refreshed.Members)
	for path := range selectedMembers {
		if _, ok := refreshedMembers[path]; !ok {
			memberErrors[path] = "selected worktree member no longer exists"
		}
	}
	for path := range refreshedMembers {
		if _, ok := selectedMembers[path]; !ok {
			memberErrors[path] = "unexpected worktree member appeared after selection"
		}
	}

	for path, current := range refreshedMembers {
		previous, ok := selectedMembers[path]
		if !ok {
			continue
		}
		var reasons []string
		if !current.EvidenceAvailable || !current.GitEvidenceAvailable || current.HardLocked || !current.Recoverable {
			reasons = append(reasons, current.Reason.Description)
		}
		if current.RepositoryID != previous.RepositoryID {
			reasons = append(reasons, fmt.Sprintf("repository changed from %q to %q", previous.RepositoryID, current.RepositoryID))
		}
		if current.HeadOID != previous.HeadOID {
			reasons = append(reasons, fmt.Sprintf("HEAD changed from %s to %s", previous.HeadOID, current.HeadOID))
		}
		if len(reasons) > 0 {
			memberErrors[path] = strings.Join(reasons, "; ")
		}
	}
	if len(memberErrors) > 0 {
		paths := make([]string, 0, len(memberErrors))
		for path := range memberErrors {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		var details []string
		for _, path := range paths {
			details = append(details, fmt.Sprintf("%s: %s", path, memberErrors[path]))
		}
		return refreshed, memberErrors, fmt.Errorf("active worktree preflight failed: %s", strings.Join(details, "; "))
	}
	return refreshed, memberErrors, nil
}

// IsActiveWorktreeTarget reports whether a scanner row is an active worktree
// cleanup unit. plain-dir, orphaned, empty, and unknown statuses are not.
func IsActiveWorktreeTarget(target types.DebrisInfo) bool {
	return target.Category == types.CategoryWorktree && target.Status == types.WorktreeActive
}

// RemoveGitWorktree runs `git worktree remove` without --force.
func RemoveGitWorktree(ctx context.Context, repositoryID, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", GitWorktreeRemoveArgs(repositoryID, worktreePath)...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if len(output) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return err
}

// GitWorktreeRemoveArgs is the non-force git worktree remove argv, including
// the --git-dir selector. Tests lock this so cleanup never gains --force.
func GitWorktreeRemoveArgs(repositoryID, worktreePath string) []string {
	return []string{"--git-dir=" + repositoryID, "worktree", "remove", worktreePath}
}

// VerifyRemovedWorktreeMember checks that the member path is gone, the
// repository no longer lists it, and captured recoverability evidence still
// holds for the preserved commit.
func VerifyRemovedWorktreeMember(ctx context.Context, member GitWorktreeMember) (bool, error) {
	pathRemoved := pathDoesNotExist(member.WorktreePath)
	listed, err := repositoryListsWorktree(ctx, member.RepositoryID, member.WorktreePath)
	if err != nil {
		return pathRemoved, err
	}
	if !pathRemoved || listed {
		return false, fmt.Errorf("member removal incomplete (path removed=%t, still listed=%t)", pathRemoved, listed)
	}

	if member.BranchRef != "" {
		output, err := runRepositoryGitCommand(ctx, member.RepositoryID, "rev-parse", "--verify", member.BranchRef+"^{commit}")
		if err != nil {
			return true, fmt.Errorf("preserved branch %s is unavailable: %w", member.BranchRef, err)
		}
		oid, err := GitOID(output)
		if err != nil || oid != member.HeadOID {
			return true, fmt.Errorf("preserved branch %s changed from %s to %s", member.BranchRef, member.HeadOID, oid)
		}
		return true, nil
	}

	localRefs, err := containingRepositoryRefs(ctx, member.RepositoryID, member.HeadOID, "refs/heads")
	if err != nil {
		return true, err
	}
	remoteRefs, err := containingRepositoryRefs(ctx, member.RepositoryID, member.HeadOID, "refs/remotes")
	if err != nil {
		return true, err
	}
	if !sharesGitRef(member.ContainingLocalRefs, localRefs) && !sharesGitRef(member.ContainingRemoteRefs, remoteRefs) {
		return true, fmt.Errorf("detached HEAD %s is no longer reachable from a captured named ref", member.HeadOID)
	}
	return true, nil
}

func repositoryListsWorktree(ctx context.Context, repositoryID, worktreePath string) (bool, error) {
	output, err := runRepositoryGitCommand(ctx, repositoryID, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, fmt.Errorf("listing repository worktrees: %w", err)
	}
	want, _ := cleaner.TargetPathKey(worktreePath)
	for _, field := range strings.Split(string(output), "\x00") {
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		listed, ok := cleaner.TargetPathKey(strings.TrimPrefix(field, "worktree "))
		if ok && listed == want {
			return true, nil
		}
	}
	return false, nil
}

func containingRepositoryRefs(ctx context.Context, repositoryID, headOID, namespace string) ([]string, error) {
	output, err := runRepositoryGitCommand(ctx, repositoryID, "for-each-ref", "--format=%(refname)", "--contains="+headOID, namespace)
	if err != nil {
		return nil, fmt.Errorf("checking refs containing %s: %w", headOID, err)
	}
	return NonEmptyGitLines(output), nil
}

func runRepositoryGitCommand(ctx context.Context, repositoryID string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"--git-dir=" + repositoryID}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) > 0 {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, err
}

func sharesGitRef(before, after []string) bool {
	afterSet := make(map[string]bool, len(after))
	for _, ref := range after {
		afterSet[ref] = true
	}
	for _, ref := range before {
		if afterSet[ref] {
			return true
		}
	}
	return false
}

func membersByPath(members []GitWorktreeMember) map[string]GitWorktreeMember {
	byPath := make(map[string]GitWorktreeMember, len(members))
	for _, member := range members {
		byPath[member.WorktreePath] = member
	}
	return byPath
}

func memberExecutions(members []GitWorktreeMember) []MemberExecution {
	out := make([]MemberExecution, len(members))
	for i, member := range members {
		out[i] = MemberExecution{WorktreePath: member.WorktreePath}
	}
	return out
}

func applyMemberErrors(members []MemberExecution, memberErrors map[string]string) []MemberExecution {
	for path, message := range memberErrors {
		found := false
		for i := range members {
			if members[i].WorktreePath == path {
				members[i].Error = message
				found = true
				break
			}
		}
		if !found {
			members = append(members, MemberExecution{WorktreePath: path, Error: message})
		}
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].WorktreePath < members[j].WorktreePath
	})
	return members
}

func callBeforeMutation(ctx context.Context, opts ExecutionOptions) error {
	if opts.BeforeMutation == nil {
		return nil
	}
	return opts.BeforeMutation(ctx)
}

func setUnitPhysicalState(result *UnitExecution, selected WorktreeCleanupUnit) {
	result.PhysicalRemoved = pathDoesNotExist(selected.TargetPath)
}

func pathDoesNotExist(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}
