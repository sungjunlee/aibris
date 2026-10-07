// Package cleanjson: cmd-to-cleanjson adapter functions moved from cmd/clean_json.go
package cleanjson

import (
	"fmt"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// BuildPlanFromCmd projects the invocation's already-built domain plan.
// Candidate mapping and selection belong to the domain planner, not JSON.
func BuildPlanFromCmd(
	result *types.ScanResult,
	source cleaner.ScanSource,
	opts types.PruneOptions,
	guidedState *worktree.GuidedCleanState,
	plan cleaner.UnifiedCleanupPlan,
	protections map[string]cleaner.CleanAuditReason,
	audit cleaner.CleanAudit,
	includePaths bool,
) (Plan, error) {
	if result == nil {
		return Plan{}, fmt.Errorf("nil cleanup scan result")
	}
	input := InputFromCmd(result, source, opts, guidedState, plan, audit, protections)
	input.IncludePaths = includePaths
	return Build(input)
}

// SnapshotComponentsFromCmd builds snapshot components from cmd-layer inputs.
func SnapshotComponentsFromCmd(
	plan cleaner.UnifiedCleanupPlan,
	auditComponents []cleaner.CleanupOverlapComponent,
	inventory []types.DebrisInfo,
	protections map[string]cleaner.CleanAuditReason,
) []SnapshotComponent {
	return SnapshotComponents(
		UnifiedPlanFromCleaner(plan),
		AuditComponentsFromCleaner(auditComponents),
		inventory,
		ProtectionsFromCleaner(protections),
	)
}

// InputFromCmd constructs a cleanjson.Input from cmd-layer types.
func InputFromCmd(
	result *types.ScanResult,
	source cleaner.ScanSource,
	opts types.PruneOptions,
	guidedState *worktree.GuidedCleanState,
	plan cleaner.UnifiedCleanupPlan,
	audit cleaner.CleanAudit,
	protections map[string]cleaner.CleanAuditReason,
) Input {
	return Input{
		Result:       result,
		Source:       SourceFromCleaner(source),
		Opts:         opts,
		Guided:       GuidedPolicyFromWorktree(guidedState),
		IncludePaths: false, // Will be set by caller
		Plan:         UnifiedPlanFromCleaner(plan),
		Evidence:     PlanEvidenceFromCleaner(plan.Evidence),
		Audit:        AuditComponentsFromCleaner(audit.Components),
		Inventory:    result.Worktrees,
		Protections:  ProtectionsFromCleaner(protections),
	}
}

// PlanEvidenceFromCleaner converts cleaner.CleanupPlanEvidence to cleanjson.PlanEvidence.
func PlanEvidenceFromCleaner(evidence cleaner.CleanupPlanEvidence) PlanEvidence {
	return PlanEvidence{
		ObservedAt:     evidence.ObservedAt,
		ProviderErrors: evidence.ProviderErrors,
	}
}

// SourceFromCleaner converts cleaner.ScanSource to cleanjson.Source.
func SourceFromCleaner(source cleaner.ScanSource) Source {
	return Source{
		Kind:       string(source.Kind),
		ObservedAt: source.ObservedAt,
	}
}

// GuidedPolicyFromWorktree converts worktree.GuidedCleanState to cleanjson.GuidedPolicy.
func GuidedPolicyFromWorktree(state *worktree.GuidedCleanState) *GuidedPolicy {
	if state == nil {
		return nil
	}
	return &GuidedPolicy{MinIdleAge: worktree.FillCleanupPolicy(state.Policy).MinIdleAge}
}

// UnifiedPlanFromCleaner converts cleaner.UnifiedCleanupPlan to cleanjson.UnifiedPlan.
func UnifiedPlanFromCleaner(plan cleaner.UnifiedCleanupPlan) UnifiedPlan {
	components := make([]PlanComponent, 0, len(plan.Components))
	for _, component := range plan.Components {
		components = append(components, PlanComponent{
			Key:           component.Key,
			CanonicalPath: component.CanonicalPath,
			Owner:         component.Owner,
			Selection:     string(component.Selection),
		})
	}
	rows := make([]PlanRow, 0, len(plan.Rows))
	for _, row := range plan.Rows {
		reasons := make([]string, 0, len(row.Reasons))
		for _, reason := range row.Reasons {
			reasons = append(reasons, string(reason.Code))
		}
		rows = append(rows, PlanRow{
			OwnerKey:        row.OwnerKey,
			Item:            row.Item,
			Relation:        string(row.Relation),
			PolicyDecision:  string(row.PolicyDecision),
			PolicySelection: string(row.PolicySelection),
			Selection:       string(row.Selection),
			Reasons:         reasons,
		})
	}
	return UnifiedPlan{Components: components, Rows: rows}
}

// AuditComponentsFromCleaner converts cleaner audit components to cleanjson format.
func AuditComponentsFromCleaner(components []cleaner.CleanupOverlapComponent) []AuditComponent {
	out := make([]AuditComponent, 0, len(components))
	for _, component := range components {
		rows := make([]AuditRow, 0, len(component.LogicalRows))
		for _, row := range component.LogicalRows {
			rows = append(rows, AuditRow{
				Item:           row.Item,
				CanonicalPath:  row.CanonicalPath,
				Relation:       string(row.Relation),
				PolicyDecision: row.PolicyDecision,
				ReasonCodes:    append([]string(nil), row.ReasonCodes...),
			})
		}
		out = append(out, AuditComponent{
			CanonicalPath: component.CanonicalPath,
			Owner:         component.Owner,
			Refusal:       component.Refusal,
			LogicalRows:   rows,
		})
	}
	return out
}

// ProtectionsFromCleaner converts cleaner.CleanAuditReason map to string map.
func ProtectionsFromCleaner(protections map[string]cleaner.CleanAuditReason) map[string]string {
	if protections == nil {
		return nil
	}
	out := make(map[string]string, len(protections))
	for key, reason := range protections {
		out[key] = string(reason)
	}
	return out
}

// PolicyForAuditItemFromCleaner is a cmd-layer adapter for policy calculation.
func PolicyForAuditItemFromCleaner(
	item types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleaner.CleanAuditReason,
	observedAt time.Time,
) (string, []string) {
	return PolicyForAuditItem(item, opts, ProtectionsFromCleaner(protectedTargets), observedAt)
}

// LogicalInputsForAuditWithPolicy creates logical inputs with policy decisions for audit.
func LogicalInputsForAuditWithPolicy(
	items []types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleaner.CleanAuditReason,
) []cleaner.CleanupOverlapLogicalInput {
	observedAt := time.Now()
	inputs := cleaner.LogicalInputsForAudit(items, opts, protectedTargets, observedAt)
	for i := range inputs {
		inputs[i].PolicyDecision, inputs[i].ReasonCodes = PolicyForAuditItemFromCleaner(
			inputs[i].Item,
			opts,
			protectedTargets,
			observedAt,
		)
	}
	return inputs
}
