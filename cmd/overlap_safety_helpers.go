package cmd

import (
	"sort"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func defaultCleanupOverlapLogicalInputs(
	targets []types.DebrisInfo,
	evidence []types.DebrisInfo,
) []cleanupOverlapLogicalInput {
	inputs := make([]cleanupOverlapLogicalInput, 0, len(targets)+len(evidence))
	for _, target := range targets {
		inputs = append(inputs, cleanupOverlapLogicalInput{
			Item:         target,
			PolicyReason: "selected cleanup target",
		})
	}
	for _, item := range evidence {
		inputs = append(inputs, cleanupOverlapLogicalInput{
			Item:         item,
			PolicyReason: item.Reason,
		})
	}
	return inputs
}

func buildCleanupOverlapComponents(
	plan cleaner.OverlapSafetyPlan,
	logicalInputs []cleanupOverlapLogicalInput,
) []cleanupOverlapComponent {
	components := make([]cleanupOverlapComponent, 0, len(plan.Components))
	for _, safety := range plan.Components {
		component := cleanupOverlapComponent{
			Key:           safety.CanonicalPath,
			CanonicalPath: safety.CanonicalPath,
			Owner:         safety.Target,
			Obligations:   append([]cleaner.AgentStateObligation(nil), safety.Obligations...),
			Refusal:       safety.Refusal,
		}
		for _, input := range logicalInputs {
			path, ok := cleaner.TargetPathKey(input.Item.Path)
			if !ok {
				continue
			}
			relation, overlaps := cleanupLogicalRelation(safety.CanonicalPath, path)
			if match, matched := cleanupSafetyMatchForInput(safety.Matches, input.Item); matched &&
				match.Relation == cleaner.OverlapRelationAmbiguous {
				relation = cleanupOverlapAmbiguous
				overlaps = true
			}
			if !overlaps {
				continue
			}
			component.LogicalRows = append(component.LogicalRows, cleanupOverlapLogicalRow{
				Item:                 input.Item,
				CanonicalPath:        path,
				Relation:             relation,
				PolicyReason:         cleanupLogicalPolicyReason(input),
				PolicyDecision:       input.PolicyDecision,
				ReasonCodes:          append([]string(nil), input.ReasonCodes...),
				L1Reason:             cleanupLogicalL1Reason(safety, input.Item, path),
				RevalidationRequired: cleanupLogicalRevalidationRequired(safety, input.Item, path),
			})
		}
		component.LogicalRows = ensureCleanupOwnerLogicalRow(component.LogicalRows, safety.Target, safety.CanonicalPath)
		sortCleanupOverlapLogicalRows(component.LogicalRows, safety.Target)
		if len(component.LogicalRows) > 0 {
			component.LogicalRows[0].PhysicalBytes = safety.Target.Size
		}
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].CanonicalPath == components[j].CanonicalPath {
			return cleaner.TargetStableKey(components[i].Owner) < cleaner.TargetStableKey(components[j].Owner)
		}
		return components[i].CanonicalPath < components[j].CanonicalPath
	})
	return components
}

func cleanupSafetyMatchForInput(
	matches []cleaner.OverlapSafetyMatch,
	item types.DebrisInfo,
) (cleaner.OverlapSafetyMatch, bool) {
	for _, match := range matches {
		if match.Item.Path == item.Path &&
			match.Item.Tool == item.Tool &&
			match.Item.ID == item.ID &&
			match.Item.Classification == item.Classification {
			return match, true
		}
	}
	return cleaner.OverlapSafetyMatch{}, false
}

func cleanupLogicalL1Reason(
	component cleaner.OverlapSafetyComponent,
	item types.DebrisInfo,
	canonicalPath string,
) string {
	for _, match := range component.Matches {
		if match.Relation == cleaner.OverlapRelationAmbiguous {
			if match.Item.Path == item.Path &&
				match.Item.Tool == item.Tool &&
				match.Item.ID == item.ID {
				return string(cleaner.OverlapSafetyAmbiguousIdentity)
			}
			continue
		}
		matchPath, ok := cleaner.TargetPathKey(match.Item.Path)
		if !ok || matchPath != canonicalPath ||
			match.Item.Tool != item.Tool ||
			match.Item.ID != item.ID {
			continue
		}
		if component.Refusal != nil {
			switch component.Refusal.Reason {
			case cleaner.OverlapSafetyCommandOverlap,
				cleaner.OverlapSafetyAmbiguousIdentity:
				return string(component.Refusal.Reason)
			case cleaner.OverlapSafetyNestedRevalidation:
				refusalPath, refusalOK := cleaner.TargetPathKey(component.Refusal.AgentStatePath)
				if refusalOK && refusalPath == canonicalPath {
					return string(component.Refusal.Reason)
				}
			}
		}
		if match.Item.Classification == types.EntryClassOrphaned {
			return "nested agent-state revalidation required"
		}
		switch match.Relation {
		case cleaner.OverlapRelationAgentStateAncestor:
			return string(cleaner.OverlapSafetyProtectedAncestor)
		case cleaner.OverlapRelationExact:
			return string(cleaner.OverlapSafetyProtectedExact)
		default:
			return string(cleaner.OverlapSafetyProtectedDescendant)
		}
	}
	return ""
}

func cleanupLogicalRevalidationRequired(
	component cleaner.OverlapSafetyComponent,
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
		if match.Relation == cleaner.OverlapRelationAmbiguous {
			continue
		}
		matchPath, ok := cleaner.TargetPathKey(match.Item.Path)
		if ok && matchPath == canonicalPath &&
			match.Item.Tool == item.Tool &&
			match.Item.ID == item.ID {
			return true
		}
	}
	return false
}

func cleanupOverlapComponentForTarget(
	selection cleanupOverlapSafetySelection,
	target types.DebrisInfo,
) (cleanupOverlapComponent, bool) {
	for _, component := range selection.Components {
		if component.Owner.Path == target.Path &&
			component.Owner.Category == target.Category &&
			component.Owner.Tool == target.Tool &&
			component.Owner.ID == target.ID {
			return component, true
		}
	}
	return cleanupOverlapComponent{}, false
}

func overlapSafetyAuditProtections(plan cleaner.OverlapSafetyPlan) map[string]cleanAuditReason {
	protections := make(map[string]cleanAuditReason)
	for _, component := range plan.Components {
		if component.Refusal == nil {
			continue
		}
		reason := cleanAuditReasonForOverlapSafety(component.Refusal.Reason)
		protections[cleanAuditItemKey(component.Target)] = reason
		for _, match := range component.Matches {
			if match.Item.Classification == types.EntryClassOrphaned {
				protections[cleanAuditItemKey(match.Item)] = reason
			}
		}
	}
	return protections
}
