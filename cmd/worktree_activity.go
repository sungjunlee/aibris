package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// WorktreeActivitySource identifies the trusted metadata source selected for
// a member's last activity. Constant order is also the deterministic tie
// precedence: Codex session, HEAD reflog, then scanner metadata.
type WorktreeActivitySource string

const (
	WorktreeActivityCodexSession WorktreeActivitySource = "codex_session"
	WorktreeActivityHeadReflog   WorktreeActivitySource = "head_reflog"
	WorktreeActivityFallback     WorktreeActivitySource = "scanner_metadata"
)

// WorktreeActivityEvidence preserves both positive timestamps and source
// availability. Available evidence may have no matching timestamp; this is
// distinct from an index or command outage.
type WorktreeActivityEvidence struct {
	Source    WorktreeActivitySource
	Timestamp time.Time
	Available bool
	Error     string
}

type worktreeActivityOptions struct {
	index        *codexActivityIndex
	indexOptions codexActivityIndexOptions
	runner       worktreeGitCommandRunner
}

// BuildWorktreeCleanupUnitsWithActivity builds cleanup units and enriches each
// member with metadata-only activity evidence. Policy and deletion decisions
// deliberately remain outside this evidence seam.
func BuildWorktreeCleanupUnitsWithActivity(ctx context.Context, items []types.DebrisInfo) ([]WorktreeCleanupUnit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	units, err := buildWorktreeCleanupUnits(ctx, items)
	if err != nil {
		return nil, err
	}
	if err := enrichWorktreeCleanupActivity(ctx, units, items, worktreeActivityOptions{}); err != nil {
		return nil, err
	}
	return units, nil
}

func enrichWorktreeCleanupActivity(ctx context.Context, units []WorktreeCleanupUnit, items []types.DebrisInfo, opts worktreeActivityOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	activity := codexActivityIndex{}
	if opts.index != nil {
		activity = *opts.index
	} else {
		activity = loadCodexActivityIndexWithOptions(ctx, opts.indexOptions)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if activity.Worktrees == nil {
		activity.Worktrees = make(map[string]codexWorktreeActivity)
	}
	if activity.Members == nil {
		activity.Members = make(map[string]codexWorktreeActivity)
	}
	if opts.runner == nil {
		opts.runner = runWorktreeGitCommand
	}

	scannerRows := cleanupUnitActivityRows(items)
	for unitIndex := range units {
		if err := ctx.Err(); err != nil {
			return err
		}
		unit := &units[unitIndex]
		unit.LastActivity = time.Time{}
		unit.ActivitySource = ""
		unit.ActivityMember = ""
		unit.ActivityAvailable = false
		unit.CodexActivityAvailable = activity.Available
		unit.CodexActivitySource = activity.Source
		unit.CodexActivityError = ""
		if activity.Err != nil {
			unit.CodexActivityError = activity.Err.Error()
		} else if !activity.Available {
			unit.CodexActivityError = errCodexActivityUnavailable.Error()
		}

		for memberIndex := range unit.Members {
			member := &unit.Members[memberIndex]
			rows := scannerRows[unit.TargetPath]
			fallback := memberFallbackActivity(member.WorktreePath, unit.TargetPath, rows)
			identity := memberCodexIdentity(member.WorktreePath, rows)
			if err := collectMemberActivity(ctx, member, fallback, identity, activity, opts.runner); err != nil {
				return err
			}
			if !member.ActivityAvailable {
				continue
			}
			if !unit.ActivityAvailable || member.LastActivity.After(unit.LastActivity) ||
				(member.LastActivity.Equal(unit.LastActivity) && member.WorktreePath < unit.ActivityMember) {
				unit.LastActivity = member.LastActivity
				unit.ActivitySource = member.ActivitySource
				unit.ActivityMember = member.WorktreePath
				unit.ActivityAvailable = true
			}
		}
	}
	return nil
}

func collectMemberActivity(ctx context.Context, member *GitWorktreeMember, fallback time.Time, identity codexActivityIdentity, activity codexActivityIndex, runner worktreeGitCommandRunner) error {
	member.LastActivity = time.Time{}
	member.ActivitySource = ""
	member.ActivityAvailable = false
	member.ActivityEvidence = nil
	member.CodexActivityAvailable = activity.Available
	member.CodexActivitySource = activity.Source
	member.CodexActivityError = ""
	if activity.Err != nil {
		member.CodexActivityError = activity.Err.Error()
	} else if !activity.Available {
		member.CodexActivityError = errCodexActivityUnavailable.Error()
	}

	session := WorktreeActivityEvidence{
		Source:    WorktreeActivityCodexSession,
		Available: activity.Available,
	}
	if !activity.Available {
		session.Error = member.CodexActivityError
	} else {
		if worktreeID, project, ok := codexActivityWorktreeFromCWD(member.WorktreePath); ok {
			identity = codexActivityIdentity{worktreeID: worktreeID, project: project}
		}
		matching, found := activity.Members[codexActivityMemberKey(identity.worktreeID, identity.project)]
		if !found {
			matching, found = activity.Worktrees[identity.worktreeID]
		}
		if found {
			session.Timestamp = matching.LatestSession
		}
	}

	reflog, err := headReflogActivity(ctx, member.WorktreePath, runner)
	if err != nil {
		return err
	}
	fallbackEvidence := WorktreeActivityEvidence{
		Source:    WorktreeActivityFallback,
		Timestamp: fallback,
		Available: !fallback.IsZero(),
	}
	if fallback.IsZero() {
		fallbackEvidence.Error = "scanner metadata unavailable"
	}

	member.ActivityEvidence = []WorktreeActivityEvidence{session, reflog, fallbackEvidence}
	for _, evidence := range member.ActivityEvidence {
		if !evidence.Available || evidence.Timestamp.IsZero() {
			continue
		}
		if !member.ActivityAvailable || evidence.Timestamp.After(member.LastActivity) {
			member.LastActivity = evidence.Timestamp
			member.ActivitySource = evidence.Source
			member.ActivityAvailable = true
		}
	}
	return nil
}

func headReflogActivity(ctx context.Context, worktreePath string, runner worktreeGitCommandRunner) (WorktreeActivityEvidence, error) {
	evidence := WorktreeActivityEvidence{Source: WorktreeActivityHeadReflog}
	commandCtx, cancel := context.WithTimeout(ctx, gitEvidenceCommandTimeout)
	defer cancel()
	output, err := runner(commandCtx, worktreePath, "reflog", "show", "-1", "--format=%ct", "HEAD")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return evidence, ctxErr
		}
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			evidence.Error = context.DeadlineExceeded.Error()
		} else {
			evidence.Error = fmt.Sprintf("HEAD reflog unavailable: %v", err)
		}
		return evidence, nil
	}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}

	value := strings.TrimSpace(string(output))
	if value == "" {
		evidence.Available = true
		return evidence, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		evidence.Error = fmt.Sprintf("HEAD reflog unavailable: invalid timestamp %q", value)
		return evidence, nil
	}
	evidence.Available = true
	evidence.Timestamp = time.Unix(seconds, 0).UTC()
	return evidence, nil
}

type codexActivityIdentity struct {
	worktreeID string
	project    string
}

func cleanupUnitActivityRows(items []types.DebrisInfo) map[string][]types.DebrisInfo {
	rows := make(map[string][]types.DebrisInfo)
	for _, item := range items {
		if item.Category != types.CategoryWorktree {
			continue
		}
		targetPath, ok := cleanTargetPathKey(item.Path)
		if !ok {
			continue
		}
		rows[targetPath] = append(rows[targetPath], item)
	}
	for targetPath := range rows {
		sort.Slice(rows[targetPath], func(i, j int) bool {
			if rows[targetPath][i].Project != rows[targetPath][j].Project {
				return rows[targetPath][i].Project < rows[targetPath][j].Project
			}
			return rows[targetPath][i].ID < rows[targetPath][j].ID
		})
	}
	return rows
}

func memberFallbackActivity(memberPath, targetPath string, rows []types.DebrisInfo) time.Time {
	project := filepath.Base(memberPath)
	var matching time.Time
	var any time.Time
	matchedProject := false
	for _, row := range rows {
		if row.ModTime.After(any) {
			any = row.ModTime
		}
		if memberPath != targetPath && row.Project == project {
			matchedProject = true
			if row.ModTime.After(matching) {
				matching = row.ModTime
			}
		}
	}
	if matchedProject {
		return matching
	}
	return any

}

func memberCodexIdentity(memberPath string, rows []types.DebrisInfo) codexActivityIdentity {
	project := filepath.Base(memberPath)
	var identities []codexActivityIdentity
	for _, row := range rows {
		if row.Source != ".codex" || row.ID == "" || row.Project == "" {
			continue
		}
		identity := codexActivityIdentity{worktreeID: row.ID, project: row.Project}
		if row.Project == project {
			return identity
		}
		identities = append(identities, identity)
	}
	if len(identities) == 1 {
		return identities[0]
	}
	return codexActivityIdentity{}
}
