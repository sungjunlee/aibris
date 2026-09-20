package cleaner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

type CleanupOverlapSafetyRuntime struct {
	OverlapRuntime
}

type CleanupOverlapSafetySelection struct {
	Plan        OverlapSafetyPlan
	Components  []CleanupOverlapComponent
	Targets     []types.DebrisInfo
	Protections map[string]CleanAuditReason
}

type OverlapSafetyEvidenceFunc func(context.Context) (OverlapSafetyEvidence, error)

func NewCleanupOverlapSafetyRuntime(
	initial OverlapSafetyEvidence,
	refresh OverlapSafetyEvidenceFunc,
	lookup AgentStateRevalidatorLookup,
) CleanupOverlapSafetyRuntime {
	return CleanupOverlapSafetyRuntime{
		OverlapRuntime: NewOverlapRuntime(initial, refresh, lookup),
	}
}

func NewCleanupOverlapSafetyRuntimeWithScan(
	ctx context.Context,
	scanEvidence OverlapSafetyEvidenceFunc,
	lookup AgentStateRevalidatorLookup,
) (CleanupOverlapSafetyRuntime, error) {
	initial, err := scanEvidence(ctx)
	if err != nil {
		return CleanupOverlapSafetyRuntime{}, err
	}
	return NewCleanupOverlapSafetyRuntime(initial, scanEvidence, lookup), nil
}

func ApplyCleanupOverlapSafety(
	ctx context.Context,
	runtime CleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
) (CleanupOverlapSafetySelection, error) {
	return ApplyCleanupOverlapSafetyWithRows(ctx, runtime, targets, nil)
}

func ApplyCleanupOverlapSafetyWithRows(
	ctx context.Context,
	runtime CleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
	logicalInputs []CleanupOverlapLogicalInput,
) (CleanupOverlapSafetySelection, error) {
	targets = NormalizeTargets(targets)
	sort.Slice(targets, func(i, j int) bool {
		left, _ := TargetPathKey(targets[i].Path)
		right, _ := TargetPathKey(targets[j].Path)
		if left == right {
			return TargetStableKey(targets[i]) < TargetStableKey(targets[j])
		}
		return left < right
	})
	plan, err := BuildOverlapSafetyPlan(ctx, runtime.Initial, targets, runtime.Lookup)
	if err != nil {
		return CleanupOverlapSafetySelection{}, err
	}
	if len(logicalInputs) == 0 {
		logicalInputs = defaultCleanupOverlapLogicalInputs(targets, runtime.Initial.Items)
	}
	return CleanupOverlapSafetySelection{
		Plan:        plan,
		Components:  BuildCleanupOverlapComponents(plan, logicalInputs),
		Targets:     plan.AllowedTargets(),
		Protections: overlapSafetyAuditProtections(plan),
	}, nil
}

type GitSafety struct {
	Protected         bool
	ProtectionReasons []string
}

type WorktreeGitInspector func(context.Context, string) GitSafety

func FilterGitUnsafeActiveWorktreeTargetsWithInspector(ctx context.Context, targets []types.DebrisInfo, inspector WorktreeGitInspector) ([]types.DebrisInfo, map[string]CleanAuditReason) {
	protections := make(map[string]CleanAuditReason)
	filtered := targets[:0]
	for _, target := range targets {
		if target.Category != types.CategoryWorktree || target.Status != types.WorktreeActive {
			filtered = append(filtered, target)
			continue
		}

		safety := inspector(ctx, target.Path)
		if !safety.Protected {
			filtered = append(filtered, target)
			continue
		}

		reason := "git status unavailable"
		if len(safety.ProtectionReasons) > 0 {
			reason = strings.Join(safety.ProtectionReasons, ", ")
		}
		protections[AuditItemKey(target)] = CleanAuditReason(reason)
	}
	return filtered, protections
}

type CleanupMutationSafety struct {
	Component OverlapSafetyComponent
	Runtime   CleanupOverlapSafetyRuntime
}

func (s CleanupMutationSafety) Validate(
	ctx context.Context,
) (OverlapSafetyValidation, error) {
	report := InitialOverlapSafetyValidation(s.Component)
	if s.Runtime.Refresh == nil {
		report.BlockingPath = s.Component.Target.Path
		report.BlockingReason = ErrIncompleteOverlapSafetyEvidence.Error()
		return report, ErrIncompleteOverlapSafetyEvidence
	}
	refreshed, err := s.Runtime.RefreshedEvidence(ctx)
	if err != nil {
		report.BlockingPath = s.Component.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	return s.Component.ValidateBeforeMutationWithReport(ctx, refreshed, s.Runtime.Lookup)
}

func InitialOverlapSafetyValidation(
	component OverlapSafetyComponent,
) OverlapSafetyValidation {
	report := OverlapSafetyValidation{
		Obligations: make([]AgentStateRevalidationOutcome, 0, len(component.Obligations)),
	}
	for _, obligation := range component.Obligations {
		report.Obligations = append(report.Obligations, AgentStateRevalidationOutcome{
			Tool:       obligation.Tool,
			EntryPath:  obligation.EntryPath,
			ProviderID: obligation.ProviderID,
			State:      AgentStateRevalidationNotAttempted,
		})
	}
	return report
}

func MutationSafetyForTarget(
	selection CleanupOverlapSafetySelection,
	runtime CleanupOverlapSafetyRuntime,
	target types.DebrisInfo,
) (*CleanupMutationSafety, error) {
	component, ok := selection.Plan.ComponentForTarget(target)
	if !ok || component.Refusal != nil {
		return nil, fmt.Errorf("overlap safety component unavailable for %q", target.Path)
	}
	return &CleanupMutationSafety{Component: component, Runtime: runtime}, nil
}

func defaultCleanupOverlapLogicalInputs(
	targets []types.DebrisInfo,
	evidence []types.DebrisInfo,
) []CleanupOverlapLogicalInput {
	inputs := make([]CleanupOverlapLogicalInput, 0, len(targets)+len(evidence))
	for _, target := range targets {
		inputs = append(inputs, CleanupOverlapLogicalInput{
			Item:         target,
			PolicyReason: "selected cleanup target",
		})
	}
	for _, item := range evidence {
		inputs = append(inputs, CleanupOverlapLogicalInput{
			Item:         item,
			PolicyReason: item.Reason,
		})
	}
	return inputs
}

func BuildCleanupOverlapComponents(
	plan OverlapSafetyPlan,
	logicalInputs []CleanupOverlapLogicalInput,
) []CleanupOverlapComponent {
	components := make([]CleanupOverlapComponent, 0, len(plan.Components))
	for _, safety := range plan.Components {
		component := CleanupOverlapComponent{
			Key:           safety.CanonicalPath,
			CanonicalPath: safety.CanonicalPath,
			Owner:         safety.Target,
			Obligations:   append([]AgentStateObligation(nil), safety.Obligations...),
			Refusal:       safety.Refusal,
		}
		for _, input := range logicalInputs {
			path, ok := TargetPathKey(input.Item.Path)
			if !ok {
				continue
			}
			relation, overlaps := CleanupLogicalRelation(safety.CanonicalPath, path)
			if match, matched := cleanupSafetyMatchForInput(safety.Matches, input.Item); matched &&
				match.Relation == OverlapRelationAmbiguous {
				relation = CleanupOverlapAmbiguous
				overlaps = true
			}
			if !overlaps {
				continue
			}
			component.LogicalRows = append(component.LogicalRows, CleanupOverlapLogicalRow{
				Item:                 input.Item,
				CanonicalPath:        path,
				Relation:             relation,
				PolicyReason:         CleanupLogicalPolicyReason(input),
				PolicyDecision:       input.PolicyDecision,
				ReasonCodes:          append([]string(nil), input.ReasonCodes...),
				L1Reason:             cleanupLogicalL1Reason(safety, input.Item, path),
				RevalidationRequired: cleanupLogicalRevalidationRequired(safety, input.Item, path),
			})
		}
		component.LogicalRows = EnsureCleanupOwnerLogicalRow(component.LogicalRows, safety.Target, safety.CanonicalPath)
		SortCleanupOverlapLogicalRows(component.LogicalRows, safety.Target)
		if len(component.LogicalRows) > 0 {
			component.LogicalRows[0].PhysicalBytes = safety.Target.Size
		}
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].CanonicalPath == components[j].CanonicalPath {
			return TargetStableKey(components[i].Owner) < TargetStableKey(components[j].Owner)
		}
		return components[i].CanonicalPath < components[j].CanonicalPath
	})
	return components
}

func cleanupSafetyMatchForInput(
	matches []OverlapSafetyMatch,
	item types.DebrisInfo,
) (OverlapSafetyMatch, bool) {
	for _, match := range matches {
		if match.Item.Path == item.Path &&
			match.Item.Tool == item.Tool &&
			match.Item.ID == item.ID &&
			match.Item.Classification == item.Classification {
			return match, true
		}
	}
	return OverlapSafetyMatch{}, false
}

func cleanupLogicalL1Reason(
	component OverlapSafetyComponent,
	item types.DebrisInfo,
	canonicalPath string,
) string {
	for _, match := range component.Matches {
		if match.Relation == OverlapRelationAmbiguous {
			if match.Item.Path == item.Path &&
				match.Item.Tool == item.Tool &&
				match.Item.ID == item.ID {
				return string(OverlapSafetyAmbiguousIdentity)
			}
			continue
		}
		matchPath, ok := TargetPathKey(match.Item.Path)
		if !ok || matchPath != canonicalPath ||
			match.Item.Tool != item.Tool ||
			match.Item.ID != item.ID {
			continue
		}
		if component.Refusal != nil {
			switch component.Refusal.Reason {
			case OverlapSafetyCommandOverlap,
				OverlapSafetyAmbiguousIdentity:
				return string(component.Refusal.Reason)
			case OverlapSafetyNestedRevalidation:
				refusalPath, refusalOK := TargetPathKey(component.Refusal.AgentStatePath)
				if refusalOK && refusalPath == canonicalPath {
					return string(component.Refusal.Reason)
				}
			}
		}
		if match.Item.Classification == types.EntryClassOrphaned {
			return "nested agent-state revalidation required"
		}
		switch match.Relation {
		case OverlapRelationAgentStateAncestor:
			return string(OverlapSafetyProtectedAncestor)
		case OverlapRelationExact:
			return string(OverlapSafetyProtectedExact)
		default:
			return string(OverlapSafetyProtectedDescendant)
		}
	}
	return ""
}

func cleanupLogicalRevalidationRequired(
	component OverlapSafetyComponent,
	item types.DebrisInfo,
	canonicalPath string,
) bool {
	if item.Category != types.CategoryAgentState ||
		item.Classification != types.EntryClassOrphaned {
		return false
	}
	for _, obligation := range component.Obligations {
		if obligation.Tool == item.Tool && obligation.EntryPath == canonicalPath {
			return true
		}
	}
	for _, match := range component.Matches {
		if match.Relation == OverlapRelationAmbiguous {
			continue
		}
		matchPath, ok := TargetPathKey(match.Item.Path)
		if ok && matchPath == canonicalPath &&
			match.Item.Tool == item.Tool &&
			match.Item.ID == item.ID {
			return true
		}
	}
	return false
}

func CleanupOverlapComponentForTarget(
	selection CleanupOverlapSafetySelection,
	target types.DebrisInfo,
) (CleanupOverlapComponent, bool) {
	for _, component := range selection.Components {
		if component.Owner.Path == target.Path &&
			component.Owner.Category == target.Category &&
			component.Owner.Tool == target.Tool &&
			component.Owner.ID == target.ID {
			return component, true
		}
	}
	return CleanupOverlapComponent{}, false
}

func overlapSafetyAuditProtections(plan OverlapSafetyPlan) map[string]CleanAuditReason {
	protections := make(map[string]CleanAuditReason)
	for _, component := range plan.Components {
		if component.Refusal == nil {
			continue
		}
		reason := AuditReasonForOverlapSafety(component.Refusal.Reason)
		protections[AuditItemKey(component.Target)] = reason
		for _, match := range component.Matches {
			if match.Item.Classification == types.EntryClassOrphaned {
				protections[AuditItemKey(match.Item)] = reason
			}
		}
	}
	return protections
}
