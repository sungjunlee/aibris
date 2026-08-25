package cleanjson

import (
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

func TestPolicySeparatesClassicAndGuidedAge(t *testing.T) {
	classicAge := 7 * 24 * time.Hour
	guidedAge := 3 * 24 * time.Hour
	guided := PolicyFor(
		types.PruneOptions{Age: classicAge},
		&GuidedPolicy{MinIdleAge: guidedAge},
	)
	if guided.MinimumAge != "7d" {
		t.Fatalf("auto-guided minimum_age = %q; want classic opts age 7d", guided.MinimumAge)
	}
	if guided.GuidedMinIdleAge != "3d" {
		t.Fatalf("auto-guided guided_min_idle_age = %q; want 3d", guided.GuidedMinIdleAge)
	}
	classic := PolicyFor(types.PruneOptions{Age: classicAge}, nil)
	if classic.GuidedMinIdleAge != "" {
		t.Fatalf("classic guided_min_idle_age = %q; want omitted", classic.GuidedMinIdleAge)
	}
}

func TestPolicyEmitsAgentStateGrace(t *testing.T) {
	policy := PolicyFor(
		types.PruneOptions{
			Age:                  7 * 24 * time.Hour,
			AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
		},
		nil,
	)
	if policy.AgentStateGrace != "1d" {
		t.Fatalf("agent_state_grace = %q; want 1d for the 24h default", policy.AgentStateGrace)
	}
	disabled := PolicyFor(types.PruneOptions{Age: 7 * 24 * time.Hour}, nil)
	if disabled.AgentStateGrace != "0d" {
		t.Fatalf("disabled agent_state_grace = %q; want 0d", disabled.AgentStateGrace)
	}
}

func TestPolicyForAuditItemMarksFreshOrphanedAgentStateReviewable(t *testing.T) {
	observedAt := time.Now()
	opts := types.PruneOptions{
		Age:                  time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	item := types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             "fresh-orphan",
		Path:           "/tmp/home/.claude/projects/fresh",
		Classification: types.EntryClassOrphaned,
		ModTime:        observedAt.Add(-2 * time.Hour),
	}
	decision, codes := PolicyForAuditItem(item, opts, nil, observedAt)
	if decision != PolicyReviewable ||
		len(codes) != 1 || codes[0] != "agent_state_min_idle_age" {
		t.Fatalf("fresh orphaned policy = %q/%v; want reviewable agent_state_min_idle_age", decision, codes)
	}

	item.ModTime = observedAt.Add(-48 * time.Hour)
	decision, codes = PolicyForAuditItem(item, opts, nil, observedAt)
	if decision != PolicyEligible ||
		len(codes) != 1 || codes[0] != "agent_state_orphaned" {
		t.Fatalf("idle orphaned policy = %q/%v; want eligible agent_state_orphaned", decision, codes)
	}
}

func TestPolicyDecisionFallbackUsesPropagatedSelectionOnly(t *testing.T) {
	row := PlanRow{
		PolicySelection: planSelected,
		Reasons:         []string{string(worktree.DecisionReasonRepositoryRetention)},
	}
	if got := policyDecisionForPlanRow(row); got != PolicyEligible {
		t.Fatalf("legacy policy fallback = %q; want %q from selected state", got, PolicyEligible)
	}
}

func TestReasonCodeAllowListPreservesKnownCodes(t *testing.T) {
	if got := ReasonCode("command_fallback_path_removal"); got != "command_fallback_path_removal" {
		t.Fatalf("command fallback reason code normalized to %q", got)
	}
	if got := ReasonCode("no_bytes_reclaimed"); got != "no_bytes_reclaimed" {
		t.Fatalf("no_bytes_reclaimed reason code normalized to %q", got)
	}
	decisionCodes := []worktree.DecisionReasonCode{
		worktree.DecisionReasonCurrentWorkingDirectory,
		worktree.DecisionReasonDirtyWorktree,
		worktree.DecisionReasonGitEvidenceUnavailable,
		worktree.DecisionReasonDetachedUnreferenced,
		worktree.DecisionReasonActivityUnavailable,
		worktree.DecisionReasonRecentActivity,
		worktree.DecisionReasonRepositoryRetention,
		worktree.DecisionReasonMinimumIdleAge,
		worktree.DecisionReasonMinimumSize,
		worktree.DecisionReasonEligible,
		worktree.DecisionReasonUniqueCommits,
		worktree.DecisionReasonMergeEvidenceUnknown,
	}
	gitEvidenceCodes := []worktree.GitEvidenceReasonCode{
		worktree.GitReasonEvidenceUnavailable,
		worktree.GitReasonDirtyWorktree,
		worktree.GitReasonAttachedBranch,
		worktree.GitReasonDetachedHeadReachable,
		worktree.GitReasonDetachedHeadUnreferenced,
	}
	for _, code := range append(
		append([]string(nil), decisionReasonStrings(decisionCodes)...),
		gitEvidenceReasonStrings(gitEvidenceCodes)...,
	) {
		if got := ReasonCode(code); got != code {
			t.Errorf("known reason code %q normalized to %q", code, got)
		}
	}

	auditReasons := []struct {
		reason string
		code   string
	}{
		{string(cleaner.EligibilityReasonFiltered), "filtered"},
		{string(cleaner.EligibilityReasonRisky), "risky_requires_opt_in"},
		{string(cleaner.EligibilityReasonActiveWorktree), "active_worktree"},
		{string(cleaner.EligibilityReasonAge), "minimum_age"},
		{string(cleaner.EligibilityReasonVolumePressure), "volume_pressure"},
		{string(cleaner.EligibilityReasonAgentStateLive), "agent_state_live"},
		{string(cleaner.EligibilityReasonAgentStateUndetermined), "agent_state_undetermined"},
		{string(cleaner.EligibilityReasonAgentStateMinIdleAge), "agent_state_min_idle_age"},
		{"path no longer exists", "missing_path"},
		{"duplicate cleanup target path", "duplicate_path"},
		{"covered by selected parent", "nested_target"},
		{"overlaps selected cleanup target", "overlap_target"},
		{"protected agent-state ancestor", "protected_agent_state_ancestor"},
		{"protected agent-state descendant or exact overlap", "protected_agent_state_descendant"},
		{"ambiguous overlap path identity", "ambiguous_overlap_identity"},
		{"cleanup command overlaps agent-state", "command_overlap"},
		{"nested agent-state revalidation refused", "nested_revalidation"},
		{"nested agent-state revalidation required", "nested_revalidation_required"},
		{"scan identity evidence unavailable", "scan_evidence_unavailable"},
		{string(cleaner.EligibilityReasonEligible), "eligible"},
		{"dirty files", "git_dirty_files"},
		{"git status unavailable", "git_evidence_unavailable"},
		{"upstream comparison unavailable", "git_upstream_unavailable"},
		{"unpushed commits", "git_unpushed_commits"},
	}
	for _, tt := range auditReasons {
		code := ReasonCodeForAuditReason(tt.reason)
		if code != tt.code || ReasonCode(code) != tt.code {
			t.Errorf("audit reason %q mapped to %q; want preserved %q", tt.reason, code, tt.code)
		}
	}
}

func decisionReasonStrings(codes []worktree.DecisionReasonCode) []string {
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		out = append(out, string(code))
	}
	return out
}

func gitEvidenceReasonStrings(codes []worktree.GitEvidenceReasonCode) []string {
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		out = append(out, string(code))
	}
	return out
}
