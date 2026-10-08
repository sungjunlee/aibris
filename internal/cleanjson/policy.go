package cleanjson

import (
	"fmt"
	"sort"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type policyInfo struct {
	Decision    string
	ReasonCodes []string
}

// PolicyFor records the prune options that produced the plan. Guided min-idle
// age is omitted unless guided review contributed candidates.
func PolicyFor(opts types.PruneOptions, guided *GuidedPolicy) Policy {
	categories := make([]string, 0, len(opts.Categories))
	for _, category := range opts.Categories {
		categories = append(categories, string(category))
	}
	tools := make([]string, 0, len(opts.Tools))
	for _, tool := range opts.Tools {
		tools = append(tools, string(tool))
	}
	sort.Strings(categories)
	sort.Strings(tools)
	policy := Policy{
		MinimumAge:             ageDisplay(opts.Age),
		AgentStateGrace:        ageDisplay(opts.AgentStateMinIdleAge),
		Categories:             categories,
		Tools:                  tools,
		Risky:                  opts.Risky,
		IncludeActiveWorktrees: opts.IncludeActiveWorktrees,
		RelaxCacheAge:          opts.RelaxCacheAge,
	}
	if guided != nil {
		policy.GuidedMinIdleAge = ageDisplay(guided.MinIdleAge)
	}
	return policy
}

// PolicyForAuditItem is the classic policy boundary for the JSON projection.
// It runs alongside the human audit, while all of the original safety inputs
// are still available. JSON rendering must only carry this recorded decision
// forward; it must not re-stat paths or re-evaluate age or filters later.
func PolicyForAuditItem(
	item types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]string,
	observedAt time.Time,
) (string, []string) {
	if protected := protectedTargets[itemKey(item)]; protected != "" {
		return PolicyProtected, []string{ReasonCodeForAuditReason(protected)}
	}
	eligible, reason := cleaner.EvaluateEligibility(item, opts, observedAt)
	if eligible {
		if reason == cleaner.EligibilityReasonVolumePressure {
			return PolicyEligible, []string{"volume_pressure"}
		}
		if item.Category == types.CategoryAgentState && item.Classification == types.EntryClassOrphaned {
			return PolicyEligible, []string{"agent_state_orphaned"}
		}
		return PolicyEligible, []string{"classic_eligible"}
	}
	switch reason {
	case cleaner.EligibilityReasonActiveWorktree,
		cleaner.EligibilityReasonAgentStateLive,
		cleaner.EligibilityReasonAgentStateUndetermined:
		return PolicyProtected, []string{ReasonCodeForEligibility(reason)}
	case cleaner.EligibilityReasonAgentStateMinIdleAge:
		// Reported rather than offered: the entry is dropped before a plan is
		// built, so it is not a togglable row anywhere. Cleaning it means
		// rerunning with a shorter or zero --agent-state-grace.
		return PolicyReviewable, []string{ReasonCodeForEligibility(reason)}
	default:
		return PolicySkipped, []string{ReasonCodeForEligibility(reason)}
	}
}

func ReasonCodeForEligibility(reason cleaner.EligibilityReason) string {
	switch reason {
	case cleaner.EligibilityReasonFiltered:
		return "filtered"
	case cleaner.EligibilityReasonRisky:
		return "risky_requires_opt_in"
	case cleaner.EligibilityReasonActiveWorktree:
		return "active_worktree"
	case cleaner.EligibilityReasonWorktreeReview:
		return "worktree_requires_review"
	case cleaner.EligibilityReasonAge:
		return "minimum_age"
	case cleaner.EligibilityReasonAgentStateLive:
		return "agent_state_live"
	case cleaner.EligibilityReasonAgentStateUndetermined:
		return "agent_state_undetermined"
	case cleaner.EligibilityReasonAgentStateMinIdleAge:
		return "agent_state_min_idle_age"
	case cleaner.EligibilityReasonVolumePressure:
		return "volume_pressure"
	case cleaner.EligibilityReasonEligible:
		return "eligible"
	default:
		return "policy_decision"
	}
}

func ReasonCodeForAuditReason(reason string) string {
	switch reason {
	case string(cleaner.EligibilityReasonFiltered):
		return "filtered"
	case string(cleaner.EligibilityReasonRisky):
		return "risky_requires_opt_in"
	case string(cleaner.EligibilityReasonActiveWorktree):
		return "active_worktree"
	case string(cleaner.EligibilityReasonAge):
		return "minimum_age"
	case string(cleaner.EligibilityReasonVolumePressure):
		return "volume_pressure"
	case string(cleaner.EligibilityReasonAgentStateLive):
		return "agent_state_live"
	case string(cleaner.EligibilityReasonAgentStateUndetermined):
		return "agent_state_undetermined"
	case string(cleaner.EligibilityReasonAgentStateMinIdleAge):
		return "agent_state_min_idle_age"
	case "path no longer exists":
		return "missing_path"
	case "duplicate cleanup target path":
		return "duplicate_path"
	case "covered by selected parent":
		return "nested_target"
	case "overlaps selected cleanup target":
		return "overlap_target"
	case "protected agent-state ancestor":
		return "protected_agent_state_ancestor"
	case "protected agent-state descendant or exact overlap":
		return "protected_agent_state_descendant"
	case "ambiguous overlap path identity":
		return "ambiguous_overlap_identity"
	case "cleanup command overlaps agent-state":
		return "command_overlap"
	case "nested agent-state revalidation refused":
		return "nested_revalidation"
	case "nested agent-state revalidation required":
		return "nested_revalidation_required"
	case "scan identity evidence unavailable":
		return "scan_evidence_unavailable"
	case string(cleaner.CleanReasonProtectPath):
		return "protect_path"
	case string(cleaner.EligibilityReasonEligible):
		return "eligible"
	case "dirty files":
		return "git_dirty_files"
	case "git status unavailable":
		return "git_evidence_unavailable"
	case "upstream comparison unavailable":
		return "git_upstream_unavailable"
	case "unpushed commits":
		return "git_unpushed_commits"
	default:
		return ReasonCode(reason)
	}
}

func reasonCodeForOverlapSafety(reason cleaner.OverlapSafetyReason) string {
	switch reason {
	case cleaner.OverlapSafetyProtectedAncestor:
		return "protected_agent_state_ancestor"
	case cleaner.OverlapSafetyProtectedDescendant, cleaner.OverlapSafetyProtectedExact:
		return "protected_agent_state_descendant"
	case cleaner.OverlapSafetyAmbiguousIdentity:
		return "ambiguous_overlap_identity"
	case cleaner.OverlapSafetyCommandOverlap:
		return "command_overlap"
	default:
		return "nested_revalidation"
	}
}

func decisionForPlanSelection(selection string) string {
	switch selection {
	case planSelected:
		return DecisionSelected
	case planLocked:
		return DecisionProtected
	default:
		return DecisionReviewable
	}
}

func policyDecisionForPlanRow(row PlanRow) string {
	if row.PolicyDecision != "" {
		return row.PolicyDecision
	}
	selection := row.PolicySelection
	if selection == "" {
		selection = row.Selection
	}
	switch selection {
	case planLocked:
		return PolicyProtected
	case planSelected:
		return PolicyEligible
	default:
		return PolicyReviewable
	}
}

func planRowReasonCodes(row PlanRow) []string {
	codes := make([]string, 0, len(row.Reasons))
	codes = append(codes, row.Reasons...)
	return UniqueReasonCodes(codes)
}

// supportedReasonCodes is the JSON boundary catalog. Domain codes come from
// worktree's policy catalog; projection and execution codes are owned here.
var supportedReasonCodes = func() map[string]struct{} {
	codes := map[string]struct{}{
		"classic_eligible":                 {},
		"agent_state_orphaned":             {},
		"contains_locked_target":           {},
		"overlaps_locked_target":           {},
		"worktree_policy_decision":         {},
		"filtered":                         {},
		"risky_requires_opt_in":            {},
		"active_worktree":                  {},
		"worktree_requires_review":         {},
		"minimum_age":                      {},
		"agent_state_live":                 {},
		"agent_state_undetermined":         {},
		"agent_state_min_idle_age":         {},
		"volume_pressure":                  {},
		"eligible":                         {},
		"missing_path":                     {},
		"duplicate_path":                   {},
		"nested_target":                    {},
		"overlap_target":                   {},
		"protected_agent_state_ancestor":   {},
		"protected_agent_state_descendant": {},
		"ambiguous_overlap_identity":       {},
		"command_overlap":                  {},
		"nested_revalidation":              {},
		"nested_revalidation_required":     {},
		"scan_evidence_unavailable":        {},
		"protect_path":                     {},
		"protected_overlap":                {},
		"not_selected":                     {},
		"policy_protected":                 {},
		"policy_decision":                  {},
		"git_dirty_files":                  {},
		"git_upstream_unavailable":         {},
		"git_unpushed_commits":             {},
		"removed":                          {},
		"partial_failure":                  {},
		"execution_failed":                 {},
		"cache_leaf_symlink":               {},
		"cleanup_recipe_changed":           {},
		"worktree_evidence_changed":        {},
		"cancelled":                        {},
		"physical_owner_present":           {},
		"no_bytes_reclaimed":               {},
		"command_fallback_path_removal":    {},
		"safety_refused":                   {},
		"execution_set_mismatch":           {},
		"plan_validation_failed":           {},
		"cancelled_before_execution":       {},
		"cancelled_after_confirmation":     {},
		"cancelled_after_execution":        {},
		"cancelled_during_confirmation":    {},
		"confirmation_cancelled":           {},
		"invalid_confirmation":             {},
		"not_confirmed":                    {},
		"execution_not_recorded":           {},
		"execution_identity_invalid":       {},
		"execution_state":                  {},
	}
	for _, code := range worktree.SupportedReasonCodes() {
		codes[code] = struct{}{}
	}
	return codes
}()

func ReasonCode(code string) string {
	if _, ok := supportedReasonCodes[code]; ok {
		return code
	}
	return "policy_decision"
}

func UniqueReasonCodes(codes []string) []string {
	seen := make(map[string]bool, len(codes))
	unique := make([]string, 0, len(codes))
	for _, code := range codes {
		code = ReasonCode(code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		unique = append(unique, code)
	}
	if len(unique) == 0 {
		return []string{"policy_decision"}
	}
	return unique
}

func ageDisplay(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}
