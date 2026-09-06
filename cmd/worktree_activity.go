package cmd

import (
	"context"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type worktreeActivityOptions struct {
	index        *codexActivityIndex
	indexOptions codexActivityIndexOptions
	runner       worktree.GitCommandRunner
}

// BuildWorktreeCleanupUnitsWithActivity builds cleanup units and enriches each
// member with metadata-only activity evidence. Policy and deletion decisions
// deliberately remain outside this evidence seam.
func BuildWorktreeCleanupUnitsWithActivity(ctx context.Context, items []types.DebrisInfo) ([]worktree.WorktreeCleanupUnit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	units, err := worktree.BuildWorktreeCleanupUnits(ctx, items)
	if err != nil {
		return nil, err
	}
	if err := enrichWorktreeCleanupActivity(ctx, units, items, worktreeActivityOptions{}); err != nil {
		return nil, err
	}
	return units, nil
}

func enrichWorktreeCleanupActivity(ctx context.Context, units []worktree.WorktreeCleanupUnit, items []types.DebrisInfo, opts worktreeActivityOptions) error {
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
		opts.runner = worktree.RunGitCommand
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
		rows := scannerRows[unit.TargetPath]
		tool := worktreeActivityTool(rows, unit.Source)
		unit.RegisteredActivityAvailable, unit.RegisteredActivitySource, unit.RegisteredActivityError = worktreeActivityAvailability(tool, unit.Source, activity)

		for memberIndex := range unit.Members {
			member := &unit.Members[memberIndex]
			fallback := memberFallbackActivity(member.WorktreePath, unit.TargetPath, rows)
			identity := memberCodexIdentity(member.WorktreePath, rows)
			if err := collectMemberActivity(ctx, member, fallback, identity, tool, unit.Source, activity, opts.runner); err != nil {
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
