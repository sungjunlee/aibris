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

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

func collectMemberActivity(ctx context.Context, member *worktree.GitWorktreeMember, fallback time.Time, identity codexActivityIdentity, tool types.Tool, source string, activity codexActivityIndex, runner worktree.GitCommandRunner) error {
	member.LastActivity = time.Time{}
	member.ActivitySource = ""
	member.ActivityAvailable = false
	member.ActivityEvidence = nil
	member.RegisteredActivityAvailable, member.RegisteredActivitySource, member.RegisteredActivityError = worktreeActivityAvailability(tool, source, activity)

	session := worktree.WorktreeActivityEvidence{
		Source:    worktree.WorktreeActivityCodexSession,
		Available: member.RegisteredActivityAvailable,
	}
	if !member.RegisteredActivityAvailable {
		session.Error = member.RegisteredActivityError
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
	fallbackEvidence := worktree.WorktreeActivityEvidence{
		Source:    worktree.WorktreeActivityFallback,
		Timestamp: fallback,
		Available: !fallback.IsZero(),
	}
	if fallback.IsZero() {
		fallbackEvidence.Error = "scanner metadata unavailable"
	}

	member.ActivityEvidence = []worktree.WorktreeActivityEvidence{session, reflog, fallbackEvidence}
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

// worktreeActivityAvailability reports whether a registered session-activity
// reader can speak for this unit. Only Codex has one today, so every other
// tool reports "not registered" — which is distinct from an outage: there is
// no reader to fail, so the unit is judged on the tool-independent evidence
// (HEAD reflog, scanner metadata) instead of being locked out of review.
func worktreeActivityAvailability(tool types.Tool, source string, activity codexActivityIndex) (bool, string, string) {
	if tool != types.ToolCodex {
		return false, worktree.ActivitySourceNotRegistered, worktree.ActivityNotRegisteredReason
	}
	return codexActivityAvailability(source, activity)
}

// worktreeActivityTool resolves the producing tool from the scanner rows that
// built the unit. A unit assembled directly from a fixture carries no rows;
// the ".codex" source still proves the registered Codex convention there.
func worktreeActivityTool(rows []types.DebrisInfo, source string) types.Tool {
	for _, row := range rows {
		if row.Tool != "" {
			return row.Tool
		}
	}
	if source == ".codex" {
		return types.ToolCodex
	}
	return types.ToolUnknown
}

func codexActivityAvailability(source string, activity codexActivityIndex) (bool, string, string) {
	if source != ".codex" {
		return false, codexActivitySourceUnavailable, fmt.Sprintf("codex activity unsupported for worktree source %q", source)
	}
	if activity.Err != nil {
		return false, activity.Source, activity.Err.Error()
	}
	if !activity.Available {
		return false, activity.Source, errCodexActivityUnavailable.Error()
	}
	return true, activity.Source, ""
}

func headReflogActivity(ctx context.Context, worktreePath string, runner worktree.GitCommandRunner) (worktree.WorktreeActivityEvidence, error) {
	evidence := worktree.WorktreeActivityEvidence{Source: worktree.WorktreeActivityHeadReflog}
	commandCtx, cancel := context.WithTimeout(ctx, worktree.GitEvidenceCommandTimeout)
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
		targetPath, ok := cleaner.TargetPathKey(item.Path)
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
