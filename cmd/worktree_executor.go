package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

type cleanExecutionState string

const (
	cleanExecutionRemoved cleanExecutionState = "removed"
	cleanExecutionPartial cleanExecutionState = "partial"
	cleanExecutionFailed  cleanExecutionState = "failed"
)

type cleanMemberExecutionReceipt struct {
	WorktreePath string
	Removed      bool
	Error        string
}

type cleanUnitExecutionReceipt struct {
	Target          types.DebrisInfo
	State           cleanExecutionState
	PhysicalRemoved bool
	FreedBytes      int64
	Members         []cleanMemberExecutionReceipt
	Error           string
}

type cleanExecutionReceipt struct {
	Units      []cleanUnitExecutionReceipt
	FreedBytes int64
}

func (r cleanExecutionReceipt) counts() (removed, partial, failed int) {
	for _, unit := range r.Units {
		switch unit.State {
		case cleanExecutionRemoved:
			removed++
		case cleanExecutionPartial:
			partial++
		case cleanExecutionFailed:
			failed++
		}
	}
	return removed, partial, failed
}

type preparedCleanTarget struct {
	Item             types.DebrisInfo
	ActiveUnit       *WorktreeCleanupUnit
	PreparationError error
}

type activeWorktreeRemover func(context.Context, string, string) error

type activeWorktreeExecutionOptions struct {
	removeWorktree activeWorktreeRemover
	removeAll      func(string) error
	getwd          func() (string, error)
	userHomeDir    func() (string, error)
}

func defaultActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	return activeWorktreeExecutionOptions{
		removeWorktree: removeGitWorktree,
		removeAll:      os.RemoveAll,
		getwd:          os.Getwd,
		userHomeDir:    os.UserHomeDir,
	}
}

// prepareCleanExecution captures the selected active worktree identity before
// confirmation. Execution refreshes and compares this evidence immediately
// before making any change.
func prepareCleanExecution(ctx context.Context, targets []types.DebrisInfo) []preparedCleanTarget {
	prepared := make([]preparedCleanTarget, 0, len(targets))
	for _, target := range targets {
		entry := preparedCleanTarget{Item: target}
		if isActiveWorktreeTarget(target) {
			units, err := buildWorktreeCleanupUnits(ctx, []types.DebrisInfo{target})
			switch {
			case err != nil:
				entry.PreparationError = err
			case len(units) != 1:
				entry.PreparationError = fmt.Errorf("expected one active cleanup unit, found %d", len(units))
			default:
				entry.ActiveUnit = &units[0]
			}
		}
		prepared = append(prepared, entry)
	}
	return prepared
}

func executeCleanTargets(ctx context.Context, targets []types.DebrisInfo) (cleanExecutionReceipt, error) {
	return executePreparedCleanTargets(ctx, prepareCleanExecution(ctx, targets), defaultActiveWorktreeExecutionOptions())
}

func executePreparedCleanTargets(ctx context.Context, targets []preparedCleanTarget, opts activeWorktreeExecutionOptions) (cleanExecutionReceipt, error) {
	if opts.removeWorktree == nil {
		opts.removeWorktree = removeGitWorktree
	}
	if opts.removeAll == nil {
		opts.removeAll = os.RemoveAll
	}
	if opts.getwd == nil {
		opts.getwd = os.Getwd
	}
	if opts.userHomeDir == nil {
		opts.userHomeDir = os.UserHomeDir
	}

	var result cleanExecutionReceipt
	var errs []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		var receipt cleanUnitExecutionReceipt
		var err error
		switch {
		case !isActiveWorktreeTarget(target.Item):
			receipt, err = executePathCleanupTarget(ctx, target.Item)
		case target.PreparationError != nil:
			receipt = failedCleanUnitReceipt(target.Item, nil, fmt.Errorf("preparing active worktree: %w", target.PreparationError))
			err = errors.New(receipt.Error)
		case target.ActiveUnit == nil:
			receipt = failedCleanUnitReceipt(target.Item, nil, errors.New("active worktree evidence unavailable"))
			err = errors.New(receipt.Error)
		default:
			receipt, err = executeActiveWorktreeUnit(ctx, target.Item, *target.ActiveUnit, opts)
		}

		result.Units = append(result.Units, receipt)
		result.FreedBytes += receipt.FreedBytes
		if err != nil {
			errs = append(errs, fmt.Errorf("cleaning %s: %w", target.Item.Path, err))
		}
	}
	if len(errs) > 0 {
		return result, fmt.Errorf("failed to remove %d item(s): %w", len(errs), errors.Join(errs...))
	}
	return result, nil
}

func executePathCleanupTarget(ctx context.Context, target types.DebrisInfo) (cleanUnitExecutionReceipt, error) {
	receipt := cleanUnitExecutionReceipt{Target: target, State: cleanExecutionFailed}
	freed, err := cleaner.ExecuteWithContext(ctx, []types.DebrisInfo{target})
	receipt.PhysicalRemoved = pathDoesNotExist(target.Path)
	if err != nil {
		receipt.Error = err.Error()
		return receipt, err
	}
	receipt.State = cleanExecutionRemoved
	receipt.FreedBytes = freed
	return receipt, nil
}

func executeActiveWorktreeUnit(ctx context.Context, target types.DebrisInfo, selected WorktreeCleanupUnit, opts activeWorktreeExecutionOptions) (cleanUnitExecutionReceipt, error) {
	receipt := cleanUnitExecutionReceipt{Target: target, State: cleanExecutionFailed}
	for _, member := range selected.Members {
		receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}

	refreshed, memberErrors, err := preflightActiveWorktreeUnit(ctx, target, selected, opts)
	if err != nil {
		applyMemberExecutionErrors(&receipt, memberErrors)
		receipt.Error = err.Error()
		return receipt, err
	}
	receipt.Members = make([]cleanMemberExecutionReceipt, len(refreshed.Members))
	for i, member := range refreshed.Members {
		receipt.Members[i].WorktreePath = member.WorktreePath
	}

	for i, member := range refreshed.Members {
		fmt.Printf("removing worktree member %d/%d: %s ...\n", i+1, len(refreshed.Members), member.WorktreePath)
		if err := opts.removeWorktree(ctx, member.RepositoryID, member.WorktreePath); err != nil {
			removed, verificationErr := verifyRemovedWorktreeMember(ctx, member)
			receipt.Members[i].Removed = removed
			receipt.Members[i].Error = err.Error()
			if verificationErr != nil {
				receipt.Members[i].Error += "; state verification: " + verificationErr.Error()
			}
			receipt.Error = fmt.Sprintf("removing active worktree member %q: %s", member.WorktreePath, receipt.Members[i].Error)
			setActiveReceiptPhysicalState(&receipt, selected)
			return receipt, errors.New(receipt.Error)
		}

		removed, postconditionErr := verifyRemovedWorktreeMember(ctx, member)
		receipt.Members[i].Removed = removed
		if postconditionErr != nil {
			receipt.Members[i].Error = postconditionErr.Error()
			receipt.Error = fmt.Sprintf("verifying active worktree member %q: %v", member.WorktreePath, postconditionErr)
			setActiveReceiptPhysicalState(&receipt, selected)
			return receipt, errors.New(receipt.Error)
		}
		fmt.Printf("removed worktree member: %s\n", member.WorktreePath)
	}

	if !pathDoesNotExist(selected.TargetPath) {
		if err := opts.removeAll(selected.TargetPath); err != nil {
			receipt.Error = fmt.Sprintf("removing cleanup unit container %q: %v", selected.TargetPath, err)
			setActiveReceiptPhysicalState(&receipt, selected)
			return receipt, errors.New(receipt.Error)
		}
	}
	if !pathDoesNotExist(selected.TargetPath) {
		receipt.Error = fmt.Sprintf("cleanup unit target still exists after removal: %q", selected.TargetPath)
		setActiveReceiptPhysicalState(&receipt, selected)
		return receipt, errors.New(receipt.Error)
	}

	receipt.State = cleanExecutionRemoved
	receipt.PhysicalRemoved = true
	receipt.FreedBytes = selected.Size
	fmt.Printf("removed: %s (%s) — %s\n", debrisExecutionName(target), target.Tool, cleaner.FormatSize(receipt.FreedBytes))
	return receipt, nil
}

func preflightActiveWorktreeUnit(ctx context.Context, target types.DebrisInfo, selected WorktreeCleanupUnit, opts activeWorktreeExecutionOptions) (WorktreeCleanupUnit, map[string]string, error) {
	memberErrors := make(map[string]string)
	if err := ctx.Err(); err != nil {
		return WorktreeCleanupUnit{}, memberErrors, err
	}
	home, err := opts.userHomeDir()
	if err != nil {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("getting home dir: %w", err)
	}
	if !cleaner.IsSafeTarget(home, target) {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("unsafe active worktree path %q rejected", target.Path)
	}
	cwd, err := opts.getwd()
	if err != nil {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("getting current working directory: %w", err)
	}
	if guidedCodexWorktreeContainsCWD(selected.TargetPath, cwd) {
		return WorktreeCleanupUnit{}, memberErrors, fmt.Errorf("current working directory is inside cleanup unit %q", selected.TargetPath)
	}

	units, err := buildWorktreeCleanupUnits(ctx, []types.DebrisInfo{target})
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

func membersByPath(members []GitWorktreeMember) map[string]GitWorktreeMember {
	byPath := make(map[string]GitWorktreeMember, len(members))
	for _, member := range members {
		byPath[member.WorktreePath] = member
	}
	return byPath
}

func applyMemberExecutionErrors(receipt *cleanUnitExecutionReceipt, memberErrors map[string]string) {
	for path, message := range memberErrors {
		found := false
		for i := range receipt.Members {
			if receipt.Members[i].WorktreePath == path {
				receipt.Members[i].Error = message
				found = true
				break
			}
		}
		if !found {
			receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: path, Error: message})
		}
	}
	sort.Slice(receipt.Members, func(i, j int) bool {
		return receipt.Members[i].WorktreePath < receipt.Members[j].WorktreePath
	})
}

func removeGitWorktree(ctx context.Context, repositoryID, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", gitWorktreeRemoveArgs(repositoryID, worktreePath)...)
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

func gitWorktreeRemoveArgs(repositoryID, worktreePath string) []string {
	return []string{"--git-dir=" + repositoryID, "worktree", "remove", worktreePath}
}

func verifyRemovedWorktreeMember(ctx context.Context, member GitWorktreeMember) (bool, error) {
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
		oid, err := gitOID(output)
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
	want, _ := cleanTargetPathKey(worktreePath)
	for _, field := range strings.Split(string(output), "\x00") {
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		listed, ok := cleanTargetPathKey(strings.TrimPrefix(field, "worktree "))
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
	return nonEmptyGitLines(output), nil
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

func setActiveReceiptPhysicalState(receipt *cleanUnitExecutionReceipt, selected WorktreeCleanupUnit) {
	receipt.PhysicalRemoved = pathDoesNotExist(selected.TargetPath)
	if receipt.PhysicalRemoved {
		receipt.FreedBytes = selected.Size
	}
	removedMembers := 0
	for _, member := range receipt.Members {
		if member.Removed {
			removedMembers++
		}
	}
	if removedMembers > 0 || receipt.PhysicalRemoved {
		receipt.State = cleanExecutionPartial
	} else {
		receipt.State = cleanExecutionFailed
	}
}

func failedCleanUnitReceipt(target types.DebrisInfo, members []GitWorktreeMember, err error) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{Target: target, State: cleanExecutionFailed, Error: err.Error()}
	for _, member := range members {
		receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}
	return receipt
}

func isActiveWorktreeTarget(target types.DebrisInfo) bool {
	return target.Category == types.CategoryWorktree && target.Status == types.WorktreeActive
}

func pathDoesNotExist(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func debrisExecutionName(target types.DebrisInfo) string {
	if target.ID != "" {
		return target.ID
	}
	if target.Project != "" {
		return target.Project
	}
	return filepath.Base(target.Path)
}
