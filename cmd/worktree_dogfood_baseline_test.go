package cmd

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestEvidenceBasedReclamationBaseline preserves the accepted dogfood shape
// when the live machine changes or activity evidence is unavailable in CI.
func TestEvidenceBasedReclamationBaseline(t *testing.T) {
	const mib int64 = 1024 * 1024
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	var units []WorktreeCleanupUnit
	hardSizes := []int64{3072, 2560, 2048, 2048, 1536, 1024, 1024, 512}
	for i, size := range hardSizes {
		unit := baselineCleanupUnit(fmt.Sprintf("locked-%02d", i), "repo-locked", size*mib, now.Add(-time.Duration(i+1)*24*time.Hour))
		switch {
		case i < 6:
			unit.Members[0].Dirty = true
			unit.Members[0].HardLocked = true
			unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonDirtyWorktree, Description: "dirty or untracked files", WorktreePath: unit.Members[0].WorktreePath}
			unit.HardLocked = true
			unit.HardLockReasons = []GitEvidenceReason{unit.Members[0].Reason}
		case i == 6:
			unit.Members[0].Recoverable = false
			unit.Members[0].HardLocked = true
			unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonDetachedHeadUnreferenced, Description: "detached HEAD not reachable from named ref", WorktreePath: unit.Members[0].WorktreePath}
			unit.HardLocked = true
			unit.HardLockReasons = []GitEvidenceReason{unit.Members[0].Reason}
		default:
			unit.Members[0].EvidenceAvailable = false
			unit.Members[0].GitEvidenceAvailable = false
			unit.Members[0].Recoverable = false
			unit.Members[0].HardLocked = true
			unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonEvidenceUnavailable, Description: "Git evidence unavailable", WorktreePath: unit.Members[0].WorktreePath}
			unit.HardLocked = true
			unit.HardLockReasons = []GitEvidenceReason{unit.Members[0].Reason}
		}
		units = append(units, unit)
	}

	retainedSizes := []int64{2048, 1536, 1024, 717, 512, 307, 205, 205, 102, 102, 103}
	retainedRepositories := []string{"repo-a", "repo-a", "repo-a", "repo-b", "repo-b", "repo-b", "repo-c", "repo-c", "repo-c", "repo-d", "repo-d"}
	for i, size := range retainedSizes {
		units = append(units, baselineCleanupUnit(fmt.Sprintf("retained-%02d", i), retainedRepositories[i], size*mib, now.Add(-time.Duration(i+5)*24*time.Hour)))
	}

	for i, size := range []int64{30, 30, 30, 30, 30, 30, 25} {
		units = append(units, baselineCleanupUnit(fmt.Sprintf("hold-%02d", i), "repo-a", size*mib, now.Add(-time.Duration(i+20)*24*time.Hour)))
	}

	recommendedSizes := []int64{2048, 1536, 1020, 922, 922, 922, 922, 922, 922, 922, 922, 922, 922}
	for i, size := range recommendedSizes {
		repository := []string{"repo-a", "repo-b", "repo-c"}[i%3]
		unit := baselineCleanupUnit(fmt.Sprintf("recommended-%02d", i), repository, size*mib, now.Add(-time.Duration(i+40)*24*time.Hour))
		switch i {
		case 0:
			unit.Members[0].Upstream = GitUpstreamMetadata{State: GitUpstreamNone}
		case 1:
			unit.Members[0].BranchRef = ""
			unit.Members[0].ContainingRemoteRefs = []string{"refs/remotes/origin/preserved"}
			unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonDetachedHeadReachable, Description: "detached HEAD reachable from named ref", WorktreePath: unit.Members[0].WorktreePath}
		case 2:
			second := baselineCleanupMember(unit.TargetPath+"/second", repository)
			unit.Members[0].WorktreePath = unit.TargetPath + "/first"
			unit.Members[0].Reason.WorktreePath = unit.Members[0].WorktreePath
			unit.Members = append(unit.Members, second)
		}
		units = append(units, unit)
	}

	policy := DefaultCleanupPolicy(now)
	plan := PlanWorktreeCleanup(units, policy)
	assertBaselineDecisionShape(t, plan, 8, 13, 11, 7)
	assertBaselineDecision(t, plan, "locked-00", DecisionLocked, 1)
	assertBaselineDecision(t, plan, "locked-06", DecisionLocked, 1)
	assertBaselineDecision(t, plan, "locked-07", DecisionLocked, 1)
	assertBaselineDecision(t, plan, "recommended-00", DecisionRecommended, 1)
	assertBaselineDecision(t, plan, "recommended-01", DecisionRecommended, 1)
	assertBaselineDecision(t, plan, "recommended-02", DecisionRecommended, 2)

	var total, locked, retained, held, recommended int64
	for _, decision := range plan.Decisions {
		total += decision.Unit.Size
		switch decision.Class {
		case DecisionLocked:
			locked += decision.Unit.Size
		case DecisionRecommended:
			recommended += decision.Unit.Size
		case DecisionReviewable:
			if decision.Reasons[0].Code == DecisionReasonRepositoryRetention {
				retained += decision.Unit.Size
			} else {
				held += decision.Unit.Size
			}
		}
	}
	if total != 34714*mib || locked != 13824*mib || recommended != 13824*mib || retained != 6861*mib || held != 205*mib {
		t.Fatalf("baseline bytes total=%d locked=%d recommended=%d retained=%d held=%d", total, locked, recommended, retained, held)
	}

	policy.MinIdleAge = 60 * 24 * time.Hour
	strict := PlanWorktreeCleanup(units, policy)
	assertBaselineDecisionShape(t, strict, 8, 0, 11, 20)
}

func baselineCleanupUnit(name, repository string, size int64, activity time.Time) WorktreeCleanupUnit {
	target := "/fixture/.codex/worktrees/" + name
	return WorktreeCleanupUnit{
		TargetPath:             target,
		Size:                   size,
		Source:                 ".codex",
		Members:                []GitWorktreeMember{baselineCleanupMember(target, repository)},
		LastActivity:           activity,
		ActivityAvailable:      true,
		CodexActivityAvailable: true,
	}
}

func baselineCleanupMember(path, repository string) GitWorktreeMember {
	return GitWorktreeMember{
		WorktreePath:         path,
		RepositoryID:         "/fixture/repositories/" + repository + "/.git",
		DisplayRepository:    "shared-display-name",
		BranchRef:            "refs/heads/" + filepath.Base(path),
		HeadOID:              "0123456789abcdef0123456789abcdef01234567",
		Upstream:             GitUpstreamMetadata{State: GitUpstreamPresent, Ref: "refs/remotes/origin/main"},
		Recoverable:          true,
		EvidenceAvailable:    true,
		GitEvidenceAvailable: true,
		Reason: GitEvidenceReason{
			Code:         GitReasonAttachedBranch,
			Description:  "local branch retained",
			WorktreePath: path,
		},
	}
}

func assertBaselineDecisionShape(t *testing.T, plan CleanupPlan, locked, recommended, retained, held int) {
	t.Helper()
	var gotLocked, gotRecommended, gotRetained, gotHeld int
	for _, decision := range plan.Decisions {
		switch decision.Class {
		case DecisionLocked:
			gotLocked++
		case DecisionRecommended:
			gotRecommended++
		case DecisionReviewable:
			if decision.Reasons[0].Code == DecisionReasonRepositoryRetention {
				gotRetained++
			} else {
				gotHeld++
			}
		}
	}
	if gotLocked != locked || gotRecommended != recommended || gotRetained != retained || gotHeld != held {
		t.Fatalf("decision shape locked=%d recommended=%d retained=%d held=%d; want %d/%d/%d/%d", gotLocked, gotRecommended, gotRetained, gotHeld, locked, recommended, retained, held)
	}
}

func assertBaselineDecision(t *testing.T, plan CleanupPlan, name string, class DecisionClass, members int) {
	t.Helper()
	wantPath := "/fixture/.codex/worktrees/" + name
	for _, decision := range plan.Decisions {
		if decision.Unit.TargetPath != wantPath {
			continue
		}
		if decision.Class != class || len(decision.Unit.Members) != members {
			t.Fatalf("decision %s class=%s members=%d; want %s/%d", name, decision.Class, len(decision.Unit.Members), class, members)
		}
		return
	}
	t.Fatalf("decision %s not found", name)
}
