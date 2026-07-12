package cmd

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultRecentActivityWindow       = 6 * time.Hour
	DefaultKeepPerRepository          = 3
	DefaultMinIdleAge                 = 3 * 24 * time.Hour
	DefaultCleanupMinSize       int64 = 256 * 1024 * 1024
)

// CleanupPolicy contains the independent inputs used to classify worktree
// cleanup units. Now is explicit so planning stays deterministic and free of
// clock or filesystem access.
type CleanupPolicy struct {
	Now                     time.Time
	CurrentWorkingDirectory string
	RecentActivityWindow    time.Duration
	KeepPerRepository       int
	MinIdleAge              time.Duration
	MinSize                 int64
}

// DefaultCleanupPolicy returns the product defaults for a caller-supplied
// reference time.
func DefaultCleanupPolicy(now time.Time) CleanupPolicy {
	return CleanupPolicy{
		Now:                  now,
		RecentActivityWindow: DefaultRecentActivityWindow,
		KeepPerRepository:    DefaultKeepPerRepository,
		MinIdleAge:           DefaultMinIdleAge,
		MinSize:              DefaultCleanupMinSize,
	}
}

// DecisionClass is the policy result before any UI selection state is added.
type DecisionClass string

const (
	DecisionLocked      DecisionClass = "locked"
	DecisionReviewable  DecisionClass = "reviewable"
	DecisionRecommended DecisionClass = "recommended"
)

// DecisionReasonCode is a stable machine-readable explanation for a cleanup
// decision. Hard-lock reasons are emitted in the declaration order below.
type DecisionReasonCode string

const (
	DecisionReasonCurrentWorkingDirectory DecisionReasonCode = "current_working_directory"
	DecisionReasonDirtyWorktree           DecisionReasonCode = "git_dirty_or_untracked"
	DecisionReasonGitEvidenceUnavailable  DecisionReasonCode = "git_evidence_unavailable"
	DecisionReasonDetachedUnreferenced    DecisionReasonCode = "git_detached_head_unreferenced"
	DecisionReasonActivityUnavailable     DecisionReasonCode = "activity_evidence_unavailable"
	DecisionReasonRecentActivity          DecisionReasonCode = "recent_activity"
	DecisionReasonRepositoryRetention     DecisionReasonCode = "retained_per_repository"
	DecisionReasonMinimumIdleAge          DecisionReasonCode = "younger_than_min_idle_age"
	DecisionReasonMinimumSize             DecisionReasonCode = "below_min_size"
	DecisionReasonEligible                DecisionReasonCode = "cleanup_recommended"
)

// DecisionReason wraps a stable code so future presentation details can be
// added without changing the planner contract.
type DecisionReason struct {
	Code         DecisionReasonCode
	Description  string
	WorktreePath string
}

type WorktreeCleanupDecision struct {
	Unit    WorktreeCleanupUnit
	Class   DecisionClass
	Reasons []DecisionReason
}

type CleanupPlan struct {
	Decisions []WorktreeCleanupDecision
}

// PlanWorktreeCleanup evaluates hard locks, ranks every unit per canonical
// repository, then classifies by hard lock, retention, idle age, and size.
func PlanWorktreeCleanup(units []WorktreeCleanupUnit, policy CleanupPolicy) CleanupPlan {
	policy = fillCleanupPolicy(policy)
	hardLockReasons := make([][]DecisionReasonCode, len(units))
	for i, unit := range units {
		hardLockReasons[i] = cleanupUnitHardLockReasonCodes(unit, policy)
	}
	retained := retainedCleanupUnits(units, policy.KeepPerRepository)

	decisions := make([]WorktreeCleanupDecision, 0, len(units))
	for i, unit := range units {
		decision := WorktreeCleanupDecision{Unit: unit}
		switch {
		case len(hardLockReasons[i]) > 0:
			decision.Class = DecisionLocked
			decision.Reasons = decisionReasons(hardLockReasons[i]...)
		case retained[cleanupUnitStableKey(unit)]:
			decision.Class = DecisionReviewable
			decision.Reasons = decisionReasons(DecisionReasonRepositoryRetention)
		case !unit.LastActivity.Before(policy.Now.Add(-policy.MinIdleAge)):
			decision.Class = DecisionReviewable
			decision.Reasons = decisionReasons(DecisionReasonMinimumIdleAge)
		case unit.Size < policy.MinSize:
			decision.Class = DecisionReviewable
			decision.Reasons = decisionReasons(DecisionReasonMinimumSize)
		default:
			decision.Class = DecisionRecommended
			decision.Reasons = decisionReasons(DecisionReasonEligible)
			decision.Reasons = append(decision.Reasons, cleanupUnitRecoverabilityReasons(unit)...)
		}
		decisions = append(decisions, decision)
	}

	sort.Slice(decisions, func(i, j int) bool {
		return cleanupUnitStableKey(decisions[i].Unit) < cleanupUnitStableKey(decisions[j].Unit)
	})
	return CleanupPlan{Decisions: decisions}
}

func fillCleanupPolicy(policy CleanupPolicy) CleanupPolicy {
	if policy.RecentActivityWindow <= 0 {
		policy.RecentActivityWindow = DefaultRecentActivityWindow
	}
	if policy.KeepPerRepository <= 0 {
		policy.KeepPerRepository = DefaultKeepPerRepository
	}
	if policy.MinIdleAge <= 0 {
		policy.MinIdleAge = DefaultMinIdleAge
	}
	if policy.MinSize <= 0 {
		policy.MinSize = DefaultCleanupMinSize
	}
	return policy
}

type repositoryCleanupUnit struct {
	key          string
	lastActivity time.Time
}

func retainedCleanupUnits(units []WorktreeCleanupUnit, keep int) map[string]bool {
	byRepository := make(map[string][]repositoryCleanupUnit)
	for _, unit := range units {
		key := cleanupUnitStableKey(unit)
		seenRepositories := make(map[string]bool)
		for _, member := range unit.Members {
			if member.RepositoryID == "" || seenRepositories[member.RepositoryID] {
				continue
			}
			seenRepositories[member.RepositoryID] = true
			byRepository[member.RepositoryID] = append(byRepository[member.RepositoryID], repositoryCleanupUnit{
				key:          key,
				lastActivity: unit.LastActivity,
			})
		}
	}

	retained := make(map[string]bool)
	for _, candidates := range byRepository {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].lastActivity.Equal(candidates[j].lastActivity) {
				return candidates[i].key < candidates[j].key
			}
			return candidates[i].lastActivity.After(candidates[j].lastActivity)
		})
		limit := keep
		if limit > len(candidates) {
			limit = len(candidates)
		}
		for i := 0; i < limit; i++ {
			retained[candidates[i].key] = true
		}
	}
	return retained
}

func cleanupUnitHardLockReasonCodes(unit WorktreeCleanupUnit, policy CleanupPolicy) []DecisionReasonCode {
	present := make(map[DecisionReasonCode]bool)
	if cleanupUnitContainsPath(unit.TargetPath, policy.CurrentWorkingDirectory) {
		present[DecisionReasonCurrentWorkingDirectory] = true
	}

	if len(unit.Members) == 0 {
		present[DecisionReasonGitEvidenceUnavailable] = true
	}
	for _, member := range unit.Members {
		if !member.EvidenceAvailable || member.RepositoryID == "" || !member.GitEvidenceAvailable || member.Reason.Code == GitReasonEvidenceUnavailable {
			present[DecisionReasonGitEvidenceUnavailable] = true
		}
		if member.Dirty || member.Reason.Code == GitReasonDirtyWorktree {
			present[DecisionReasonDirtyWorktree] = true
		}
		if member.GitEvidenceAvailable && (!member.Recoverable || member.Reason.Code == GitReasonDetachedHeadUnreferenced) {
			present[DecisionReasonDetachedUnreferenced] = true
		}
	}
	for _, reason := range unit.HardLockReasons {
		switch reason.Code {
		case GitReasonEvidenceUnavailable:
			present[DecisionReasonGitEvidenceUnavailable] = true
		case GitReasonDirtyWorktree:
			present[DecisionReasonDirtyWorktree] = true
		case GitReasonDetachedHeadUnreferenced:
			present[DecisionReasonDetachedUnreferenced] = true
		}
	}
	if unit.HardLocked && !present[DecisionReasonGitEvidenceUnavailable] && !present[DecisionReasonDirtyWorktree] && !present[DecisionReasonDetachedUnreferenced] {
		present[DecisionReasonGitEvidenceUnavailable] = true
	}
	if !unit.ActivityAvailable || !unit.CodexActivityAvailable {
		present[DecisionReasonActivityUnavailable] = true
	}
	if unit.ActivityAvailable && unit.LastActivity.After(policy.Now.Add(-policy.RecentActivityWindow)) {
		present[DecisionReasonRecentActivity] = true
	}

	order := []DecisionReasonCode{
		DecisionReasonCurrentWorkingDirectory,
		DecisionReasonDirtyWorktree,
		DecisionReasonGitEvidenceUnavailable,
		DecisionReasonDetachedUnreferenced,
		DecisionReasonRecentActivity,
		DecisionReasonActivityUnavailable,
	}
	reasons := make([]DecisionReasonCode, 0, len(order))
	for _, code := range order {
		if present[code] {
			reasons = append(reasons, code)
		}
	}
	return reasons
}

func cleanupUnitContainsPath(target, path string) bool {
	if target == "" || path == "" {
		return false
	}
	var ok bool
	target, ok = cleanTargetPathKey(target)
	if !ok {
		return false
	}
	path, ok = cleanTargetPathKey(path)
	if !ok {
		return false
	}
	if target == path {
		return true
	}
	relative, err := filepath.Rel(target, path)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func cleanupUnitStableKey(unit WorktreeCleanupUnit) string {
	return filepath.Clean(unit.TargetPath)
}

func cleanupUnitRecoverabilityReasons(unit WorktreeCleanupUnit) []DecisionReason {
	members := append([]GitWorktreeMember(nil), unit.Members...)
	sort.Slice(members, func(i, j int) bool {
		return members[i].WorktreePath < members[j].WorktreePath
	})

	reasons := make([]DecisionReason, 0, len(members))
	for _, member := range members {
		reason := DecisionReason{WorktreePath: member.WorktreePath}
		switch member.Reason.Code {
		case GitReasonAttachedBranch:
			reason.Code = DecisionReasonCode(GitReasonAttachedBranch)
			reason.Description = member.Reason.Description
			if reason.Description == "" {
				reason.Description = "local branch retained: " + member.BranchRef
			}
		case GitReasonDetachedHeadReachable:
			reason.Code = DecisionReasonCode(GitReasonDetachedHeadReachable)
			reason.Description = member.Reason.Description
			if reason.Description == "" {
				reason.Description = "detached HEAD reachable from named ref"
			}
		default:
			continue
		}
		reasons = append(reasons, reason)
	}
	return reasons
}

func decisionReasons(codes ...DecisionReasonCode) []DecisionReason {
	reasons := make([]DecisionReason, 0, len(codes))
	for _, code := range codes {
		reasons = append(reasons, DecisionReason{Code: code, Description: decisionReasonDescription(code)})
	}
	return reasons
}

func decisionReasonDescription(code DecisionReasonCode) string {
	switch code {
	case DecisionReasonCurrentWorkingDirectory:
		return "current working directory is inside cleanup unit"
	case DecisionReasonGitEvidenceUnavailable:
		return "Git evidence unavailable"
	case DecisionReasonDirtyWorktree:
		return "dirty or untracked files"
	case DecisionReasonDetachedUnreferenced:
		return "detached HEAD not reachable from named ref"
	case DecisionReasonActivityUnavailable:
		return "activity evidence unavailable"
	case DecisionReasonRecentActivity:
		return "activity within recent safety window"
	case DecisionReasonRepositoryRetention:
		return "retained among the most recent units for a repository"
	case DecisionReasonMinimumIdleAge:
		return "younger than minimum idle age"
	case DecisionReasonMinimumSize:
		return "below minimum recommendation size"
	case DecisionReasonEligible:
		return "eligible for cleanup recommendation"
	default:
		return "cleanup policy decision"
	}
}
