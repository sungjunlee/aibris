package worktree

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// WorktreeCleanupUnit is one canonical physical deletion target. A unit may
// contain more than one Git worktree member, but its size is counted once.
type WorktreeCleanupUnit struct {
	TargetPath                  string
	Size                        int64
	Source                      string
	Members                     []GitWorktreeMember
	LastActivity                time.Time
	ActivitySource              WorktreeActivitySource
	ActivityMember              string
	ActivityAvailable           bool
	RegisteredActivityAvailable bool
	RegisteredActivitySource    string
	RegisteredActivityError     string
	HardLocked                  bool
	HardLockReasons             []GitEvidenceReason
}

// GitWorktreeMember identifies an actual Git worktree contained by a cleanup
// unit: direct, one-level nested, or registered two-level
// <owner>/<leaf>/<checkout>.
type GitWorktreeMember struct {
	WorktreePath         string
	RepositoryID         string
	DisplayRepository    string
	BranchRef            string
	HeadOID              string
	ContainingLocalRefs  []string
	ContainingRemoteRefs []string
	Upstream             GitUpstreamMetadata
	Dirty                bool
	Recoverable          bool
	HardLocked           bool
	Reason               GitEvidenceReason
	// EvidenceAvailable reports whether repository identity metadata resolved.
	// GitEvidenceAvailable separately reports whether the recoverability
	// inspection completed; both are required for a member to pass hard safety.
	EvidenceAvailable           bool
	EvidenceError               string
	GitEvidenceAvailable        bool
	GitEvidenceError            string
	// GitStatusError records a failed full-checkout untracked status walk
	// that did not invalidate HEAD/ref evidence (strip baseline only).
	GitStatusError              string
	LastActivity                time.Time
	ActivitySource              WorktreeActivitySource
	ActivityAvailable           bool
	ActivityEvidence            []WorktreeActivityEvidence
	RegisteredActivityAvailable bool
	RegisteredActivitySource    string
	RegisteredActivityError     string
	DefaultBranchUniqueness     DefaultBranchUniqueness
}

type worktreeCleanupUnitRows struct {
	targetPath string
	items      []types.DebrisInfo
}

// BuildWorktreeCleanupUnits adapts scanner rows into deterministic physical
// cleanup units without changing the persisted DebrisInfo or scan JSON shape.
// Scan DebrisInfo.Status is the only active/orphaned/plain-dir source after
// discovery: review-only statuses never become cleanup units.
func BuildWorktreeCleanupUnits(ctx context.Context, items []types.DebrisInfo) ([]WorktreeCleanupUnit, error) {
	grouped := make(map[string][]types.DebrisInfo)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Category != types.CategoryWorktree {
			continue
		}
		targetPath, ok := cleaner.TargetPathKey(item.Path)
		if !ok {
			continue
		}
		grouped[targetPath] = append(grouped[targetPath], item)
	}

	groups := make([]worktreeCleanupUnitRows, 0, len(grouped))
	for targetPath, rows := range grouped {
		groups = append(groups, worktreeCleanupUnitRows{targetPath: targetPath, items: rows})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].targetPath < groups[j].targetPath
	})

	units := make([]WorktreeCleanupUnit, 0, len(groups))
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cleanupUnitHasReviewOnlyStatus(group.items) {
			continue
		}
		members, err := discoverGitWorktreeMembers(ctx, group.targetPath)
		if err != nil {
			return nil, fmt.Errorf("enumerating Git worktree members under %q: %w", group.targetPath, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(members) == 0 {
			continue
		}
		hardLockReasons := cleanupUnitHardLockReasons(members)
		units = append(units, WorktreeCleanupUnit{
			TargetPath:      group.targetPath,
			Size:            cleanupUnitSize(group.items),
			Source:          cleanupUnitSource(group.items),
			Members:         members,
			HardLocked:      len(hardLockReasons) > 0,
			HardLockReasons: hardLockReasons,
		})
	}
	return units, nil
}

func BuildGitWorktreeMember(ctx context.Context, worktreePath string) GitWorktreeMember {
	member := GitWorktreeMember{
		WorktreePath: worktreePath,
		Upstream:     GitUpstreamMetadata{State: GitUpstreamUnavailable},
	}
	repositoryID, displayRepository, err := resolveRepositoryIdentity(worktreePath)
	if err != nil {
		member.EvidenceError = err.Error()
		markGitEvidenceUnavailable(&member, err)
		return member
	}

	member.RepositoryID = repositoryID
	member.DisplayRepository = displayRepository
	member.EvidenceAvailable = true
	inspectGitWorktreeEvidenceWithTimeout(ctx, &member)
	return member
}

// BuildGitStripBaselineMember inspects a checkout for the strip baseline.
// Recoverability comes from HEAD/ref inspection; a full-checkout untracked
// status failure (for example a timeout caused by a large untracked tree
// elsewhere) is recorded in GitStatusError without making the Git evidence
// unavailable. Real Git failures still fail closed.
func BuildGitStripBaselineMember(ctx context.Context, worktreePath string) GitWorktreeMember {
	member := GitWorktreeMember{
		WorktreePath: worktreePath,
		Upstream:     GitUpstreamMetadata{State: GitUpstreamUnavailable},
	}
	repositoryID, displayRepository, err := resolveRepositoryIdentity(worktreePath)
	if err != nil {
		member.EvidenceError = err.Error()
		markGitEvidenceUnavailable(&member, err)
		return member
	}

	member.RepositoryID = repositoryID
	member.DisplayRepository = displayRepository
	member.EvidenceAvailable = true
	inspectGitStripBaselineEvidence(ctx, &member, RunGitCommand)
	return member
}

func inspectGitWorktreeEvidenceWithTimeout(ctx context.Context, member *GitWorktreeMember) {
	evidenceCtx, cancel := context.WithTimeout(ctx, GitEvidenceCommandTimeout)
	defer cancel()
	inspectGitWorktreeEvidence(evidenceCtx, member, RunGitCommand)
}

func cleanupUnitHardLockReasons(members []GitWorktreeMember) []GitEvidenceReason {
	reasons := make([]GitEvidenceReason, 0, len(members))
	for _, member := range members {
		if member.HardLocked {
			reasons = append(reasons, member.Reason)
		}
	}
	return reasons
}

func cleanupUnitSize(items []types.DebrisInfo) int64 {
	var size int64
	for _, item := range items {
		if item.Size > size {
			size = item.Size
		}
	}
	return size
}

func cleanupUnitSource(items []types.DebrisInfo) string {
	sources := make(map[string]bool)
	for _, item := range items {
		if item.Source != "" {
			sources[item.Source] = true
		}
	}
	ordered := make([]string, 0, len(sources))
	for source := range sources {
		ordered = append(ordered, source)
	}
	sort.Strings(ordered)
	if len(ordered) == 0 {
		return ""
	}
	return ordered[0]
}
