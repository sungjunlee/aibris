package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/types"
)

const cleanJSONSchemaVersion = cleanjson.SchemaVersion

const (
	cleanJSONDecisionSelected   = cleanjson.DecisionSelected
	cleanJSONDecisionReviewable = cleanjson.DecisionReviewable
	cleanJSONDecisionProtected  = cleanjson.DecisionProtected
	cleanJSONDecisionSkipped    = cleanjson.DecisionSkipped

	cleanJSONPolicyEligible    = cleanjson.PolicyEligible
	cleanJSONPolicyRecommended = cleanjson.PolicyRecommended
	cleanJSONPolicyReviewable  = cleanjson.PolicyReviewable
	cleanJSONPolicyProtected   = cleanjson.PolicyProtected
	cleanJSONPolicySkipped     = cleanjson.PolicySkipped
)

type (
	cleanJSONPlan              = cleanjson.Plan
	cleanJSONPhysicalTarget    = cleanjson.PhysicalTarget
	cleanJSONRow               = cleanjson.Row
	cleanJSONSnapshotComponent = cleanjson.SnapshotComponent
)

func failCleanJSON(message string) {
	fmt.Fprintf(os.Stderr, "error: %s\n", message)
	os.Exit(1)
}

func buildCleanJSONPlan(
	ctx context.Context,
	result *types.ScanResult,
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
	protections map[string]cleanAuditReason,
	audit cleanAudit,
) (cleanJSONPlan, error) {
	if result == nil {
		return cleanJSONPlan{}, fmt.Errorf("nil cleanup scan result")
	}
	if err := requireCompleteScan(result); err != nil {
		return cleanJSONPlan{}, err
	}
	observedAt := source.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	evidence := cleanupPlanEvidence(result, source, observedAt)
	candidates := cleanJSONPlanCandidates(guidedState, classicTargets, opts)
	plan, err := BuildUnifiedCleanupPlan(ctx, candidates, evidence)
	if err != nil {
		return cleanJSONPlan{}, err
	}
	return cleanjson.Build(cleanJSONInput(result, source, opts, guidedState, plan, evidence, audit, protections))
}

func renderCleanJSONPlanDocument(
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	evidence CleanupPlanEvidence,
	components []cleanJSONSnapshotComponent,
) cleanJSONPlan {
	return cleanjson.Render(cleanjson.Input{
		Source:       cleanJSONSource(source),
		Opts:         opts,
		Guided:       cleanJSONGuidedPolicy(guidedState),
		Evidence:     cleanJSONEvidenceFromPlan(evidence),
		IncludePaths: cleanIncludePaths,
	}, components)
}

func encodeCleanJSON(output io.Writer, document cleanJSONPlan) error {
	return cleanjson.Encode(output, document)
}

func cleanJSONPlanCandidates(
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
	opts types.PruneOptions,
) []CleanupPlanCandidate {
	classicTargets = cleaner.NormalizeTargets(classicTargets)
	candidates := make([]CleanupPlanCandidate, 0, guidedCandidateCount(guidedState)+len(classicTargets))
	if guidedState != nil {
		candidates = append(candidates, guidedCleanupPlanCandidates(*guidedState)...)
	}
	candidates = append(candidates, ClassicCleanupPlanCandidates(classicTargets, opts)...)

	return candidates
}

func buildCleanJSONSnapshotComponents(
	plan UnifiedCleanupPlan,
	auditComponents []cleanupOverlapComponent,
	inventory []types.DebrisInfo,
	protections map[string]cleanAuditReason,
) []cleanJSONSnapshotComponent {
	return cleanjson.SnapshotComponents(
		cleanJSONUnifiedPlan(plan),
		cleanJSONAuditComponents(auditComponents),
		inventory,
		cleanJSONProtections(protections),
	)
}

func cleanJSONPolicyForAuditItem(
	item types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleanAuditReason,
	observedAt time.Time,
) (string, []string) {
	return cleanjson.PolicyForAuditItem(item, opts, cleanJSONProtections(protectedTargets), observedAt)
}

func uniqueCleanJSONReasonCodes(codes []string) []string {
	return cleanjson.UniqueReasonCodes(codes)
}

func cleanJSONRowIdentityKey(item types.DebrisInfo) string {
	return cleanjson.RowIdentityKey(item)
}

func cleanJSONInput(
	result *types.ScanResult,
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	plan UnifiedCleanupPlan,
	evidence CleanupPlanEvidence,
	audit cleanAudit,
	protections map[string]cleanAuditReason,
) cleanjson.Input {
	return cleanjson.Input{
		Result:       result,
		Source:       cleanJSONSource(source),
		Opts:         opts,
		Guided:       cleanJSONGuidedPolicy(guidedState),
		IncludePaths: cleanIncludePaths,
		Plan:         cleanJSONUnifiedPlan(plan),
		Evidence:     cleanJSONEvidenceFromPlan(evidence),
		Audit:        cleanJSONAuditComponents(audit.Components),
		Inventory:    result.Worktrees,
		Protections:  cleanJSONProtections(protections),
	}
}

func cleanJSONSource(source scanSource) cleanjson.Source {
	return cleanjson.Source{
		Kind:       string(source.Kind),
		ObservedAt: source.ObservedAt,
	}
}

func cleanJSONGuidedPolicy(state *guidedCleanState) *cleanjson.GuidedPolicy {
	if state == nil {
		return nil
	}
	return &cleanjson.GuidedPolicy{MinIdleAge: fillCleanupPolicy(state.Policy).MinIdleAge}
}

func cleanJSONEvidenceFromPlan(evidence CleanupPlanEvidence) cleanjson.PlanEvidence {
	return cleanjson.PlanEvidence{
		ObservedAt:     evidence.ObservedAt,
		ProviderErrors: evidence.ProviderErrors,
	}
}

func cleanJSONUnifiedPlan(plan UnifiedCleanupPlan) cleanjson.UnifiedPlan {
	components := make([]cleanjson.PlanComponent, 0, len(plan.Components))
	for _, component := range plan.Components {
		components = append(components, cleanjson.PlanComponent{
			Key:           component.Key,
			CanonicalPath: component.CanonicalPath,
			Owner:         component.Owner,
			Selection:     string(component.Selection),
		})
	}
	rows := make([]cleanjson.PlanRow, 0, len(plan.Rows))
	for _, row := range plan.Rows {
		reasons := make([]string, 0, len(row.Reasons))
		for _, reason := range row.Reasons {
			reasons = append(reasons, string(reason.Code))
		}
		rows = append(rows, cleanjson.PlanRow{
			OwnerKey:        row.OwnerKey,
			Item:            row.Item,
			Relation:        string(row.Relation),
			PolicyDecision:  string(row.PolicyDecision),
			PolicySelection: string(row.PolicySelection),
			Selection:       string(row.Selection),
			Reasons:         reasons,
		})
	}
	return cleanjson.UnifiedPlan{Components: components, Rows: rows}
}

func cleanJSONAuditComponents(components []cleanupOverlapComponent) []cleanjson.AuditComponent {
	out := make([]cleanjson.AuditComponent, 0, len(components))
	for _, component := range components {
		rows := make([]cleanjson.AuditRow, 0, len(component.LogicalRows))
		for _, row := range component.LogicalRows {
			rows = append(rows, cleanjson.AuditRow{
				Item:           row.Item,
				CanonicalPath:  row.CanonicalPath,
				Relation:       string(row.Relation),
				PolicyDecision: row.PolicyDecision,
				ReasonCodes:    append([]string(nil), row.ReasonCodes...),
			})
		}
		out = append(out, cleanjson.AuditComponent{
			CanonicalPath: component.CanonicalPath,
			Owner:         component.Owner,
			Refusal:       component.Refusal,
			LogicalRows:   rows,
		})
	}
	return out
}

func cleanJSONProtections(protections map[string]cleanAuditReason) map[string]string {
	if protections == nil {
		return nil
	}
	out := make(map[string]string, len(protections))
	for key, reason := range protections {
		out[key] = string(reason)
	}
	return out
}
