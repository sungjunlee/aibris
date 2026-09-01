package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

// cleanupOverlapSafetyRuntime is a thin cobra/audit wrapper around the
// overlap runtime owned by internal/cleaner; the refresh memoization,
// fingerprinting, and batch refresh logic live in cleaner.OverlapRuntime.
type cleanupOverlapSafetyRuntime struct {
	cleaner.OverlapRuntime
}

type cleanupOverlapSafetySelection struct {
	Plan        cleaner.OverlapSafetyPlan
	Components  []cleanupOverlapComponent
	Targets     []types.DebrisInfo
	Protections map[string]cleanAuditReason
}

type cleanupOverlapRelation string

const (
	cleanupOverlapOwner      cleanupOverlapRelation = "physical-owner"
	cleanupOverlapExact      cleanupOverlapRelation = "exact-path"
	cleanupOverlapDescendant cleanupOverlapRelation = "nested-descendant"
	cleanupOverlapAncestor   cleanupOverlapRelation = "containing-ancestor"
	cleanupOverlapAmbiguous  cleanupOverlapRelation = "ambiguous-path"
)

type cleanupOverlapLogicalInput struct {
	Item           types.DebrisInfo
	PolicyReason   string
	PolicyDecision string
	ReasonCodes    []string
}

type cleanupOverlapLogicalRow struct {
	Key                  string
	Item                 types.DebrisInfo
	CanonicalPath        string
	Relation             cleanupOverlapRelation
	PolicyReason         string
	PolicyDecision       string
	ReasonCodes          []string
	L1Reason             string
	RevalidationRequired bool
	PhysicalBytes        int64
	DiscoveryOrdinal     int
}

type cleanupOverlapComponent struct {
	Key           string
	CanonicalPath string
	Owner         types.DebrisInfo
	LogicalRows   []cleanupOverlapLogicalRow
	Obligations   []cleaner.AgentStateObligation
	Refusal       *cleaner.OverlapSafetyRefusal
}

func newDefaultCleanupOverlapSafetyRuntime(
	ctx context.Context,
) (cleanupOverlapSafetyRuntime, error) {
	agentStateScanner := scanner.New(adapter.DefaultAgentStateProviders())
	agentStateScanner.ErrorWriter = io.Discard
	return newCleanupOverlapSafetyRuntime(
		ctx,
		agentStateScanner,
		adapter.AgentStateRevalidatorRegistrationFor,
	)
}

func newCleanupOverlapSafetyRuntime(
	ctx context.Context,
	agentStateScanner *scanner.Scanner,
	lookup cleaner.AgentStateRevalidatorLookup,
) (cleanupOverlapSafetyRuntime, error) {
	scanEvidence := func(ctx context.Context) (cleaner.OverlapSafetyEvidence, error) {
		if agentStateScanner == nil {
			return cleaner.OverlapSafetyEvidence{}, cleaner.ErrIncompleteOverlapSafetyEvidence
		}
		result, err := agentStateScanner.Scan(ctx)
		if err != nil {
			return cleaner.OverlapSafetyEvidence{}, err
		}
		if result == nil {
			return cleaner.OverlapSafetyEvidence{}, cleaner.ErrIncompleteOverlapSafetyEvidence
		}
		return cleaner.OverlapSafetyEvidence{
			Items:          append([]types.DebrisInfo(nil), result.Worktrees...),
			ProviderErrors: append([]types.ScanProviderError(nil), result.ProviderErrors...),
			Complete:       len(result.ProviderErrors) == 0,
		}, nil
	}

	initial, err := scanEvidence(ctx)
	if err != nil {
		return cleanupOverlapSafetyRuntime{}, err
	}
	return cleanupOverlapSafetyRuntime{
		OverlapRuntime: cleaner.NewOverlapRuntime(initial, scanEvidence, lookup),
	}, nil
}

func applyCleanupOverlapSafety(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
) (cleanupOverlapSafetySelection, error) {
	return applyCleanupOverlapSafetyWithRows(ctx, runtime, targets, nil)
}

func applyCleanupOverlapSafetyWithRows(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
	logicalInputs []cleanupOverlapLogicalInput,
) (cleanupOverlapSafetySelection, error) {
	targets = cleaner.NormalizeTargets(targets)
	sort.Slice(targets, func(i, j int) bool {
		left, _ := cleaner.TargetPathKey(targets[i].Path)
		right, _ := cleaner.TargetPathKey(targets[j].Path)
		if left == right {
			return cleaner.TargetStableKey(targets[i]) < cleaner.TargetStableKey(targets[j])
		}
		return left < right
	})
	plan, err := cleaner.BuildOverlapSafetyPlan(ctx, runtime.Initial, targets, runtime.Lookup)
	if err != nil {
		return cleanupOverlapSafetySelection{}, err
	}
	if len(logicalInputs) == 0 {
		logicalInputs = defaultCleanupOverlapLogicalInputs(targets, runtime.Initial.Items)
	}
	return cleanupOverlapSafetySelection{
		Plan:        plan,
		Components:  buildCleanupOverlapComponents(plan, logicalInputs),
		Targets:     plan.AllowedTargets(),
		Protections: overlapSafetyAuditProtections(plan),
	}, nil
}

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

func cleanupLogicalRelation(ownerPath, rowPath string) (cleanupOverlapRelation, bool) {
	switch {
	case ownerPath == rowPath:
		return cleanupOverlapExact, true
	case cleaner.PathContains(ownerPath, rowPath):
		return cleanupOverlapDescendant, true
	case cleaner.PathContains(rowPath, ownerPath):
		return cleanupOverlapAncestor, true
	default:
		return "", false
	}
}

func cleanupLogicalPolicyReason(input cleanupOverlapLogicalInput) string {
	if input.PolicyReason != "" {
		return input.PolicyReason
	}
	if input.Item.Reason != "" {
		return input.Item.Reason
	}
	if input.Item.Category == types.CategoryAgentState {
		switch input.Item.Classification {
		case types.EntryClassOrphaned:
			return "recorded working directory is absent"
		case types.EntryClassLive:
			return "live agent-state protected"
		default:
			return "undetermined agent-state protected"
		}
	}
	return "discovered cleanup evidence"
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

func ensureCleanupOwnerLogicalRow(
	rows []cleanupOverlapLogicalRow,
	owner types.DebrisInfo,
	canonicalPath string,
) []cleanupOverlapLogicalRow {
	for _, row := range rows {
		if row.CanonicalPath == canonicalPath &&
			cleaner.TargetStableKey(row.Item) == cleaner.TargetStableKey(owner) {
			return rows
		}
	}
	return append(rows, cleanupOverlapLogicalRow{
		Item:          owner,
		CanonicalPath: canonicalPath,
		Relation:      cleanupOverlapExact,
		PolicyReason:  "selected cleanup target",
	})
}

func sortCleanupOverlapLogicalRows(
	rows []cleanupOverlapLogicalRow,
	owner types.DebrisInfo,
) {
	sort.SliceStable(rows, func(i, j int) bool {
		return cleanupOverlapLogicalRowStableKey(rows[i], owner) <
			cleanupOverlapLogicalRowStableKey(rows[j], owner)
	})
	ordinals := make(map[string]int)
	ownerAssigned := false
	for i := range rows {
		baseKey := cleanupOverlapLogicalRowStableKey(rows[i], owner)
		ordinals[baseKey]++
		rows[i].DiscoveryOrdinal = ordinals[baseKey]
		if !ownerAssigned &&
			rows[i].Relation == cleanupOverlapExact &&
			cleaner.TargetStableKey(rows[i].Item) == cleaner.TargetStableKey(owner) {
			rows[i].Relation = cleanupOverlapOwner
			ownerAssigned = true
		}
		rows[i].Key = fmt.Sprintf("%s#%d", baseKey, rows[i].DiscoveryOrdinal)
	}
}

func cleanupOverlapLogicalRowStableKey(
	row cleanupOverlapLogicalRow,
	owner types.DebrisInfo,
) string {
	ownerRank := "1"
	if row.CanonicalPath != "" &&
		cleaner.TargetStableKey(row.Item) == cleaner.TargetStableKey(owner) {
		ownerRank = "0"
	}
	return strings.Join([]string{
		ownerRank,
		row.CanonicalPath,
		string(row.Relation),
		string(row.Item.Category),
		string(row.Item.Tool),
		row.Item.ID,
		row.Item.Path,
		string(row.Item.Classification),
		row.PolicyReason,
		row.L1Reason,
	}, "\x00")
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

func cleanAuditReasonForOverlapSafety(reason cleaner.OverlapSafetyReason) cleanAuditReason {
	switch reason {
	case cleaner.OverlapSafetyProtectedAncestor:
		return cleanReasonProtectedAgentStateAncestor
	case cleaner.OverlapSafetyProtectedDescendant, cleaner.OverlapSafetyProtectedExact:
		return cleanReasonProtectedAgentStateDescendant
	case cleaner.OverlapSafetyAmbiguousIdentity:
		return cleanReasonAmbiguousOverlapIdentity
	case cleaner.OverlapSafetyCommandOverlap:
		return cleanReasonCommandOverlap
	default:
		return cleanReasonNestedRevalidation
	}
}

func mergeCleanAuditProtections(
	protectionSets ...map[string]cleanAuditReason,
) map[string]cleanAuditReason {
	merged := make(map[string]cleanAuditReason)
	for _, protections := range protectionSets {
		for key, reason := range protections {
			merged[key] = reason
		}
	}
	return merged
}

type worktreeGitInspector func(context.Context, string) worktreeGitSafety

func filterGitUnsafeActiveWorktreeTargets(ctx context.Context, targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	return filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx, targets, inspectActiveWorktreeCleanupSafety)
}

func filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx context.Context, targets []types.DebrisInfo, inspector worktreeGitInspector) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	protections := make(map[string]cleanAuditReason)
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

		reason := gitProtectionGitStatusUnavailable
		if len(safety.ProtectionReasons) > 0 {
			reason = strings.Join(safety.ProtectionReasons, ", ")
		}
		protections[cleanAuditItemKey(target)] = cleanAuditReason(reason)
	}
	return filtered, protections
}

func printOverlapSafetyRefusals(selection cleanupOverlapSafetySelection) {
	for _, component := range selection.Components {
		if component.Refusal != nil {
			fmt.Printf("  safety  refused %s\n", component.Refusal)
			printCleanupComponentLineage(component, "    ")
		}
	}
}

type cleanupMutationSafety struct {
	component cleaner.OverlapSafetyComponent
	runtime   cleanupOverlapSafetyRuntime
}

func (s cleanupMutationSafety) validate(
	ctx context.Context,
) (cleaner.OverlapSafetyValidation, error) {
	report := initialOverlapSafetyValidation(s.component)
	if s.runtime.Refresh == nil {
		report.BlockingPath = s.component.Target.Path
		report.BlockingReason = cleaner.ErrIncompleteOverlapSafetyEvidence.Error()
		return report, cleaner.ErrIncompleteOverlapSafetyEvidence
	}
	refreshed, err := s.runtime.RefreshedEvidence(ctx)
	if err != nil {
		report.BlockingPath = s.component.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	return s.component.ValidateBeforeMutationWithReport(ctx, refreshed, s.runtime.Lookup)
}

func initialOverlapSafetyValidation(
	component cleaner.OverlapSafetyComponent,
) cleaner.OverlapSafetyValidation {
	report := cleaner.OverlapSafetyValidation{
		Obligations: make([]cleaner.AgentStateRevalidationOutcome, 0, len(component.Obligations)),
	}
	for _, obligation := range component.Obligations {
		report.Obligations = append(report.Obligations, cleaner.AgentStateRevalidationOutcome{
			Tool:       obligation.Tool,
			EntryPath:  obligation.EntryPath,
			ProviderID: obligation.ProviderID,
			State:      cleaner.AgentStateRevalidationNotAttempted,
		})
	}
	return report
}

func mutationSafetyForTarget(
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
	target types.DebrisInfo,
) (*cleanupMutationSafety, error) {
	component, ok := selection.Plan.ComponentForTarget(target)
	if !ok || component.Refusal != nil {
		return nil, fmt.Errorf("overlap safety component unavailable for %q", target.Path)
	}
	return &cleanupMutationSafety{component: component, runtime: runtime}, nil
}
