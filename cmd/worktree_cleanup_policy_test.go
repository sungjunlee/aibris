package cmd

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const cleanupPolicyMiB int64 = 1024 * 1024

func TestPlanWorktreeCleanupPolicyMatrix(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	defaultPolicy := DefaultCleanupPolicy(now)

	tests := []struct {
		name   string
		units  []WorktreeCleanupUnit
		policy CleanupPolicy
		want   map[string]cleanupPolicyWant
	}{
		{
			name: "fourth old safe large unit is recommended",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("first", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
				cleanupPolicyUnit("second", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
				cleanupPolicyUnit("third", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
				cleanupPolicyUnit("fourth", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
			},
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"first":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"second": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"third":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"fourth": {DecisionRecommended, []DecisionReasonCode{DecisionReasonEligible, DecisionReasonCode(GitReasonAttachedBranch)}},
			},
		},
		{
			name: "recent activity is locked regardless of idle age",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("recent", now.Add(-time.Hour), 512*cleanupPolicyMiB, "/repos/recent/.git"),
			},
			policy: func() CleanupPolicy {
				policy := defaultPolicy
				policy.MinIdleAge = time.Minute
				return policy
			}(),
			want: map[string]cleanupPolicyWant{
				"recent": {DecisionLocked, []DecisionReasonCode{DecisionReasonRecentActivity}},
			},
		},
		{
			name: "hard locked retained unit occupies rank without backfill",
			units: []WorktreeCleanupUnit{
				cleanupPolicyDirtyUnit(cleanupPolicyUnit("locked-first", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/no-backfill/.git")),
				cleanupPolicyUnit("second", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/no-backfill/.git"),
				cleanupPolicyUnit("third", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/no-backfill/.git"),
				cleanupPolicyUnit("fourth", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/no-backfill/.git"),
			},
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"locked-first": {DecisionLocked, []DecisionReasonCode{DecisionReasonDirtyWorktree}},
				"second":       {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"third":        {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"fourth":       {DecisionRecommended, []DecisionReasonCode{DecisionReasonEligible, DecisionReasonCode(GitReasonAttachedBranch)}},
			},
		},
		{
			name: "same display basename does not merge canonical repositories",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("a-first", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/shared/.git"),
				cleanupPolicyUnit("a-second", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/shared/.git"),
				cleanupPolicyUnit("a-third", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/shared/.git"),
				cleanupPolicyUnit("a-fourth", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/shared/.git"),
				cleanupPolicyUnit("b-first", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/b/shared/.git"),
				cleanupPolicyUnit("b-second", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/b/shared/.git"),
				cleanupPolicyUnit("b-third", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/b/shared/.git"),
				cleanupPolicyUnit("b-fourth", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/b/shared/.git"),
			},
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"a-first":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"a-second": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"a-third":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"a-fourth": {DecisionRecommended, []DecisionReasonCode{DecisionReasonEligible, DecisionReasonCode(GitReasonAttachedBranch)}},
				"b-first":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"b-second": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"b-third":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"b-fourth": {DecisionRecommended, []DecisionReasonCode{DecisionReasonEligible, DecisionReasonCode(GitReasonAttachedBranch)}},
			},
		},
		{
			name: "multi repository unit is retained when top three anywhere",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("a-first", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/.git"),
				cleanupPolicyUnit("a-second", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/.git"),
				cleanupPolicyUnit("a-third", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/.git"),
				cleanupPolicyUnit("shared", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/a/.git", "/repos/b/.git"),
			},
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"a-first":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"a-second": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"a-third":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"shared":   {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
			},
		},
		{
			name: "missing Git or activity evidence fails closed",
			units: []WorktreeCleanupUnit{
				cleanupPolicyMissingGitUnit(cleanupPolicyUnit("missing-git", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/missing-git/.git")),
				cleanupPolicyMissingActivityUnit(cleanupPolicyUnit("missing-activity", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/missing-activity/.git")),
			},
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"missing-git":      {DecisionLocked, []DecisionReasonCode{DecisionReasonGitEvidenceUnavailable}},
				"missing-activity": {DecisionLocked, []DecisionReasonCode{DecisionReasonActivityUnavailable}},
			},
		},
		{
			name: "activity ties use stable cleanup unit key",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("d", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/ties/.git"),
				cleanupPolicyUnit("c", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/ties/.git"),
				cleanupPolicyUnit("b", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/ties/.git"),
				cleanupPolicyUnit("a", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/ties/.git"),
			},
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"a": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"b": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"c": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"d": {DecisionRecommended, []DecisionReasonCode{DecisionReasonEligible, DecisionReasonCode(GitReasonAttachedBranch)}},
			},
		},
		{
			name: "idle age precedes size after retention",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("age-retained", now.Add(-24*time.Hour), 512*cleanupPolicyMiB, "/repos/age/.git"),
				cleanupPolicyUnit("age-held", now.Add(-48*time.Hour), 512*cleanupPolicyMiB, "/repos/age/.git"),
				cleanupPolicyUnit("size-retained", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/size/.git"),
				cleanupPolicyUnit("size-held", now.Add(-5*24*time.Hour), 128*cleanupPolicyMiB, "/repos/size/.git"),
			},
			policy: func() CleanupPolicy {
				policy := defaultPolicy
				policy.KeepPerRepository = 1
				return policy
			}(),
			want: map[string]cleanupPolicyWant{
				"age-retained":  {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"age-held":      {DecisionReviewable, []DecisionReasonCode{DecisionReasonMinimumIdleAge}},
				"size-retained": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"size-held":     {DecisionReviewable, []DecisionReasonCode{DecisionReasonMinimumSize}},
			},
		},
		{
			name: "exact minimum idle age remains reviewable",
			units: []WorktreeCleanupUnit{
				cleanupPolicyUnit("retained", now.Add(-24*time.Hour), 512*cleanupPolicyMiB, "/repos/idle-boundary/.git"),
				cleanupPolicyUnit("boundary", now.Add(-DefaultMinIdleAge), 512*cleanupPolicyMiB, "/repos/idle-boundary/.git"),
			},
			policy: func() CleanupPolicy {
				policy := defaultPolicy
				policy.KeepPerRepository = 1
				return policy
			}(),
			want: map[string]cleanupPolicyWant{
				"retained": {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"boundary": {DecisionReviewable, []DecisionReasonCode{DecisionReasonMinimumIdleAge}},
			},
		},
		{
			name: "historical session presence has no policy effect beyond last activity",
			units: func() []WorktreeCleanupUnit {
				units := []WorktreeCleanupUnit{
					cleanupPolicyUnit("first", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/history/.git"),
					cleanupPolicyUnit("second", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/history/.git"),
					cleanupPolicyUnit("third", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/history/.git"),
					cleanupPolicyUnit("historical", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/history/.git"),
				}
				units[3].Members[0].ActivityEvidence = []WorktreeActivityEvidence{{
					Source: WorktreeActivityCodexSession, Timestamp: now.Add(-30 * 24 * time.Hour), Available: true,
				}}
				return units
			}(),
			policy: defaultPolicy,
			want: map[string]cleanupPolicyWant{
				"first":      {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"second":     {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"third":      {DecisionReviewable, []DecisionReasonCode{DecisionReasonRepositoryRetention}},
				"historical": {DecisionRecommended, []DecisionReasonCode{DecisionReasonEligible, DecisionReasonCode(GitReasonAttachedBranch)}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := PlanWorktreeCleanup(tt.units, tt.policy)
			assertCleanupPolicyPlan(t, plan, tt.want)

			reversed := append([]WorktreeCleanupUnit(nil), tt.units...)
			for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
				reversed[left], reversed[right] = reversed[right], reversed[left]
			}
			if got := PlanWorktreeCleanup(reversed, tt.policy); !reflect.DeepEqual(got, plan) {
				t.Fatalf("plan depends on input order:\nfirst:    %+v\nreversed: %+v", plan, got)
			}
		})
	}
}

func TestPlanWorktreeCleanupOrdersHardLockReasonsAndDecisions(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	unit := cleanupPolicyUnit("zeta", now.Add(-time.Hour), 512*cleanupPolicyMiB, "/repos/zeta/.git")
	unit.HardLocked = true
	unit.CodexActivityAvailable = false
	unit.Members[0].EvidenceAvailable = false
	unit.Members[0].RepositoryID = ""
	unit.Members[0].Dirty = true
	unit.Members[0].Recoverable = false
	unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonDirtyWorktree}

	alpha := cleanupPolicyUnit("alpha", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git")
	policy := DefaultCleanupPolicy(now)
	policy.CurrentWorkingDirectory = unit.TargetPath + "/nested"
	plan := PlanWorktreeCleanup([]WorktreeCleanupUnit{unit, alpha}, policy)

	if got := []string{cleanupPolicyUnitName(plan.Decisions[0].Unit), cleanupPolicyUnitName(plan.Decisions[1].Unit)}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("decision order = %v; want [alpha zeta]", got)
	}
	wantCodes := []DecisionReasonCode{
		DecisionReasonCurrentWorkingDirectory,
		DecisionReasonDirtyWorktree,
		DecisionReasonGitEvidenceUnavailable,
		DecisionReasonDetachedUnreferenced,
		DecisionReasonRecentActivity,
		DecisionReasonActivityUnavailable,
	}
	if got := cleanupPolicyReasonCodes(plan.Decisions[1]); !reflect.DeepEqual(got, wantCodes) {
		t.Fatalf("hard-lock reason order = %v; want %v", got, wantCodes)
	}
	for _, decision := range plan.Decisions {
		for _, reason := range decision.Reasons {
			if reason.Description == "" {
				t.Errorf("%s reason %q has no human description", cleanupPolicyUnitName(decision.Unit), reason.Code)
			}
		}
	}
}

func TestPlanWorktreeCleanupUsesDefaultPolicyValues(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	policy := fillCleanupPolicy(CleanupPolicy{Now: now})
	if policy.RecentActivityWindow != 6*time.Hour || policy.KeepPerRepository != 3 || policy.MinIdleAge != 3*24*time.Hour || policy.MinSize != 256*cleanupPolicyMiB {
		t.Fatalf("default policy = %+v", policy)
	}
}

type cleanupPolicyWant struct {
	class   DecisionClass
	reasons []DecisionReasonCode
}

func cleanupPolicyUnit(name string, activity time.Time, size int64, repositoryIDs ...string) WorktreeCleanupUnit {
	target := "/home/user/.codex/worktrees/" + name
	members := make([]GitWorktreeMember, 0, len(repositoryIDs))
	for i, repositoryID := range repositoryIDs {
		members = append(members, GitWorktreeMember{
			WorktreePath:           target + "/member-" + string(rune('a'+i)),
			RepositoryID:           repositoryID,
			DisplayRepository:      "shared",
			BranchRef:              "refs/heads/fixture",
			Recoverable:            true,
			EvidenceAvailable:      true,
			GitEvidenceAvailable:   true,
			LastActivity:           activity,
			ActivityAvailable:      true,
			CodexActivityAvailable: true,
			Reason: GitEvidenceReason{
				Code: GitReasonAttachedBranch,
			},
		})
	}
	return WorktreeCleanupUnit{
		TargetPath:             target,
		Size:                   size,
		Source:                 ".codex",
		Members:                members,
		LastActivity:           activity,
		ActivityAvailable:      true,
		CodexActivityAvailable: true,
	}
}

func cleanupPolicyDirtyUnit(unit WorktreeCleanupUnit) WorktreeCleanupUnit {
	unit.HardLocked = true
	unit.Members[0].Dirty = true
	unit.Members[0].HardLocked = true
	unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonDirtyWorktree}
	unit.HardLockReasons = []GitEvidenceReason{{Code: GitReasonDirtyWorktree}}
	return unit
}

func cleanupPolicyMissingGitUnit(unit WorktreeCleanupUnit) WorktreeCleanupUnit {
	unit.HardLocked = true
	unit.Members[0].GitEvidenceAvailable = false
	unit.Members[0].Recoverable = false
	unit.Members[0].HardLocked = true
	unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonEvidenceUnavailable}
	unit.HardLockReasons = []GitEvidenceReason{{Code: GitReasonEvidenceUnavailable}}
	return unit
}

func cleanupPolicyMissingActivityUnit(unit WorktreeCleanupUnit) WorktreeCleanupUnit {
	unit.CodexActivityAvailable = false
	unit.CodexActivityError = "fixture activity index unavailable"
	for i := range unit.Members {
		unit.Members[i].CodexActivityAvailable = false
	}
	return unit
}

func assertCleanupPolicyPlan(t *testing.T, plan CleanupPlan, want map[string]cleanupPolicyWant) {
	t.Helper()
	if len(plan.Decisions) != len(want) {
		t.Fatalf("decisions = %d; want %d (%+v)", len(plan.Decisions), len(want), plan.Decisions)
	}
	gotOrder := make([]string, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		name := cleanupPolicyUnitName(decision.Unit)
		gotOrder = append(gotOrder, name)
		expected, ok := want[name]
		if !ok {
			t.Errorf("unexpected decision for %q: %+v", name, decision)
			continue
		}
		if decision.Class != expected.class {
			t.Errorf("%s class = %q; want %q", name, decision.Class, expected.class)
		}
		if got := cleanupPolicyReasonCodes(decision); !reflect.DeepEqual(got, expected.reasons) {
			t.Errorf("%s reasons = %v; want %v", name, got, expected.reasons)
		}
		for _, reason := range decision.Reasons {
			if reason.Description == "" {
				t.Errorf("%s reason %q has no human description", name, reason.Code)
			}
			if reason.Code == DecisionReasonCode(GitReasonAttachedBranch) && !strings.Contains(reason.Description, "local branch retained") {
				t.Errorf("%s attached-branch reason = %q; want retained branch explanation", name, reason.Description)
			}
		}
	}
	wantOrder := append([]string(nil), gotOrder...)
	sort.Strings(wantOrder)
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("decision order = %v; want stable key order %v", gotOrder, wantOrder)
	}
}

func cleanupPolicyReasonCodes(decision WorktreeCleanupDecision) []DecisionReasonCode {
	codes := make([]DecisionReasonCode, 0, len(decision.Reasons))
	for _, reason := range decision.Reasons {
		codes = append(codes, reason.Code)
	}
	return codes
}

func cleanupPolicyUnitName(unit WorktreeCleanupUnit) string {
	for i := len(unit.TargetPath) - 1; i >= 0; i-- {
		if unit.TargetPath[i] == '/' {
			return unit.TargetPath[i+1:]
		}
	}
	return unit.TargetPath
}
