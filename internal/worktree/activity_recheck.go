package worktree

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/types"
)

// ErrActivityEvidenceChanged identifies a guided activity barrier refusal.
// It includes missing review evidence and a reader that is now unavailable.
var ErrActivityEvidenceChanged = errors.New("worktree activity evidence changed")

// ActivityReview preserves the evidence used by review, independently of the
// Git unit rebuilt during preparation. Its private copies cannot be changed by
// selection, replanning, or subsequent enrichment of the original unit.
type ActivityReview struct {
	unit   WorktreeCleanupUnit
	items  []types.DebrisInfo
	policy CleanupPolicy
}

func CaptureActivityReview(unit WorktreeCleanupUnit, items []types.DebrisInfo, policy CleanupPolicy) *ActivityReview {
	unit.Members = append([]GitWorktreeMember(nil), unit.Members...)
	for i := range unit.Members {
		unit.Members[i].ActivityEvidence = append([]WorktreeActivityEvidence(nil), unit.Members[i].ActivityEvidence...)
	}
	rows := cleanupUnitActivityRows(filterActiveWorktrees(items))[unit.TargetPath]
	return &ActivityReview{unit: unit, items: append([]types.DebrisInfo(nil), rows...), policy: FillCleanupPolicy(policy)}
}

// Validate refreshes before the member loop, before each member removal and
// before the owner removal. Enrichment uses review's exact session lookup,
// source availability, reflog and fallback rules, and the policy helper applies
// the same activity locks with live time.
func (r *ActivityReview) Validate(ctx context.Context, selected WorktreeCleanupUnit) error {
	if r == nil || len(r.items) == 0 || r.unit.TargetPath != selected.TargetPath || len(r.unit.Members) == 0 || len(r.unit.Members) != len(selected.Members) {
		return fmt.Errorf("%w: review identity unavailable for %q", ErrActivityEvidenceChanged, selected.TargetPath)
	}
	previous := membersByPath(r.unit.Members)
	if len(previous) != len(selected.Members) {
		return fmt.Errorf("%w: duplicate review member identity", ErrActivityEvidenceChanged)
	}
	selectedMembers := membersByPath(selected.Members)
	if len(selectedMembers) != len(selected.Members) {
		return fmt.Errorf("%w: duplicate prepared member identity", ErrActivityEvidenceChanged)
	}
	for _, member := range selected.Members {
		if _, ok := previous[member.WorktreePath]; !ok {
			return fmt.Errorf("%w: member %q was not reviewed", ErrActivityEvidenceChanged, member.WorktreePath)
		}
	}
	if locks := cleanupUnitActivityLockReasonCodes(r.unit, r.policy); len(locks) > 0 {
		return fmt.Errorf("%w: review activity locked (%v)", ErrActivityEvidenceChanged, locks)
	}
	current := r.unit
	current.Members = append([]GitWorktreeMember(nil), r.unit.Members...)
	units := []WorktreeCleanupUnit{current}
	if err := EnrichActivity(ctx, units, r.items, ActivityOptions{IndexOptions: codexactivity.IndexOptions{RequireRefresh: true}}); err != nil {
		return err
	}
	current = units[0]
	if cleanupUnitHasRegisteredActivitySource(current) != cleanupUnitHasRegisteredActivitySource(r.unit) {
		return fmt.Errorf("%w: keeping %q: reviewed activity source unavailable", ErrActivityEvidenceChanged, current.TargetPath)
	}
	policy := r.policy
	policy.Now = time.Now()
	if locks := cleanupUnitActivityLockReasonCodes(current, policy); len(locks) > 0 {
		return fmt.Errorf("%w: keeping %q: %s", ErrActivityEvidenceChanged, current.TargetPath, GuidedCleanupDecisionReason(lockedCleanupDecision(current, locks)))
	}
	for _, member := range current.Members {
		before, beforeOK := sessionActivityEvidence(previous[member.WorktreePath])
		after, afterOK := sessionActivityEvidence(member)
		if !beforeOK || !afterOK || (cleanupUnitHasRegisteredActivitySource(current) && (!before.Available || !after.Available)) {
			return fmt.Errorf("%w: keeping %q: member activity unavailable", ErrActivityEvidenceChanged, member.WorktreePath)
		}
		if after.Timestamp.After(before.Timestamp) {
			return fmt.Errorf("%w: keeping %q: latest session advanced from %s to %s", ErrActivityEvidenceChanged, member.WorktreePath, before.Timestamp.Format(time.RFC3339Nano), after.Timestamp.Format(time.RFC3339Nano))
		}
	}
	return nil
}

func sessionActivityEvidence(member GitWorktreeMember) (WorktreeActivityEvidence, bool) {
	for _, evidence := range member.ActivityEvidence {
		if evidence.Source == WorktreeActivityCodexSession {
			return evidence, true
		}
	}
	return WorktreeActivityEvidence{}, false
}
