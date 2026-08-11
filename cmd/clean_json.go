package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

const cleanJSONSchemaVersion = 1

const (
	cleanJSONDecisionSelected   = "selected"
	cleanJSONDecisionReviewable = "reviewable"
	cleanJSONDecisionProtected  = "protected"
	cleanJSONDecisionSkipped    = "skipped"

	cleanJSONPolicyEligible    = "eligible"
	cleanJSONPolicyRecommended = "recommended"
	cleanJSONPolicyReviewable  = "reviewable"
	cleanJSONPolicyProtected   = "protected"
	cleanJSONPolicySkipped     = "skipped"
)

// cleanJSONPlan is deliberately private. The wire contract must not expose
// the canonical paths, stable keys, or execution-layer receipt types used to
// construct it.
type cleanJSONPlan struct {
	SchemaVersion   int                       `json:"schema_version"`
	DocumentType    string                    `json:"document_type"`
	Mode            string                    `json:"mode"`
	PathsIncluded   bool                      `json:"paths_included"`
	Evidence        cleanJSONEvidence         `json:"evidence"`
	Policy          cleanJSONPolicy           `json:"policy"`
	Totals          cleanJSONTotals           `json:"totals"`
	PhysicalTargets []cleanJSONPhysicalTarget `json:"physical_targets"`
	Rows            []cleanJSONRow            `json:"rows"`
}

type cleanJSONEvidence struct {
	Complete   bool   `json:"complete"`
	Source     string `json:"source"`
	ObservedAt string `json:"observed_at"`
}

type cleanJSONPolicy struct {
	MinimumAge             string   `json:"minimum_age"`
	GuidedMinIdleAge       string   `json:"guided_min_idle_age,omitempty"`
	AgentStateGrace        string   `json:"agent_state_grace"`
	Categories             []string `json:"categories"`
	Tools                  []string `json:"tools"`
	Risky                  bool     `json:"risky"`
	IncludeActiveWorktrees bool     `json:"include_active_worktrees"`
}

type cleanJSONTotals struct {
	VisibleRows     int   `json:"visible_rows"`
	PhysicalTargets int   `json:"physical_targets"`
	PhysicalBytes   int64 `json:"physical_bytes"`
	Selected        int   `json:"selected"`
	SelectedBytes   int64 `json:"selected_bytes"`
	Reviewable      int   `json:"reviewable"`
	ReviewableBytes int64 `json:"reviewable_bytes"`
	Protected       int   `json:"protected"`
	ProtectedBytes  int64 `json:"protected_bytes"`
	Skipped         int   `json:"skipped"`
	SkippedBytes    int64 `json:"skipped_bytes"`
}

type cleanJSONPhysicalTarget struct {
	ID          string  `json:"id"`
	Decision    string  `json:"decision"`
	Bytes       int64   `json:"bytes"`
	Category    string  `json:"category"`
	Tool        string  `json:"tool"`
	CleanupKind string  `json:"cleanup_kind"`
	Path        *string `json:"path,omitempty"`
}

type cleanJSONRow struct {
	ID               string    `json:"id"`
	PhysicalTargetID string    `json:"physical_target_id"`
	Relation         string    `json:"relation"`
	PolicyDecision   string    `json:"policy_decision"`
	Decision         string    `json:"decision"`
	Category         string    `json:"category"`
	Tool             string    `json:"tool"`
	ReasonCodes      []string  `json:"reason_codes"`
	Path             *string   `json:"path,omitempty"`
	Project          *string   `json:"project,omitempty"`
	CleanupCommand   *[]string `json:"cleanup_command,omitempty"`
}

type cleanJSONSnapshotComponent struct {
	Key             string
	Owner           types.DebrisInfo
	Decision        string
	AccountingBytes int64
	Rows            []cleanJSONSnapshotRow
}

type cleanJSONSnapshotRow struct {
	Item           types.DebrisInfo
	Relation       string
	PolicyDecision string
	Decision       string
	ReasonCodes    []string
	SortKey        string
}

type cleanJSONPolicyInfo struct {
	Decision    string
	ReasonCodes []string
}

func runCleanJSON(cmd *cobra.Command) {
	if !cleanDryRun && cleanGuide {
		failCleanJSON("non-dry-run --json cannot use --guide")
	}
	age, err := parseAge(cleanAge)
	if err != nil {
		failCleanJSON("invalid --age value")
	}
	if age <= 0 {
		failCleanJSON("--age must be positive")
	}
	agentStateGrace, err := parseAge(cleanAgentStateGrace)
	if err != nil {
		failCleanJSON("invalid --agent-state-grace value")
	}
	if agentStateGrace < 0 {
		failCleanJSON("--agent-state-grace must be non-negative")
	}

	guidedAge := guidedCleanAge(cmd, age)
	if cleanGuide {
		age = applyGuidedCleanDefaults(cmd, age)
		guidedAge = age
	}
	categories, err := parseCleanCategories(cleanCategory)
	if err != nil {
		failCleanJSON("invalid --category selector")
	}
	tools, err := parseCleanTools(cleanTools)
	if err != nil {
		failCleanJSON("invalid --tool selector")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	roots, err := scanner.NormalizeRoots(cleanRoots)
	if err != nil {
		failCleanJSON("invalid scan root")
	}
	result, source, err := scanForCleanQuiet(ctx, roots)
	if err != nil {
		if errors.Is(err, errIncompleteCleanupScan) {
			failCleanJSON("cleanup requires a complete scan")
		}
		failCleanJSON("cleanup scan failed")
	}
	refreshCleanupInventoryMetadata(result.Worktrees)
	overlapSafety, err := newDefaultCleanupOverlapSafetyRuntime(ctx)
	if err != nil {
		failCleanJSON("cleanup safety preparation failed")
	}

	experience := cleanExperienceClassic
	var guidedState guidedCleanState
	var reason string
	if cleanDryRun {
		usefulGuidedCodexReview := false
		if shouldPrepareGuidedClean(cmd) {
			usefulGuidedCodexReview = hasGuidedCodexCleanupPressure(ctx, result.Worktrees)
		}
		if cleanGuide || usefulGuidedCodexReview {
			guidedState, err = buildGuidedCleanState(ctx, result, source, guidedAge, "")
			if err != nil {
				failCleanJSON("guided cleanup planning failed")
			}
		}
		experience, reason, err = chooseCleanExperience(cleanExperienceInputFromCommand(cmd, usefulGuidedCodexReview))
		if err != nil {
			failCleanJSON("invalid cleanup route")
		}
	}

	opts := types.PruneOptions{
		Age:                    age,
		Categories:             categories,
		Tools:                  tools,
		DryRun:                 cleanDryRun,
		Risky:                  cleanRisky,
		Force:                  cleanForce,
		IncludeActiveWorktrees: cleanIncludeActiveWorktrees,
		AgentStateMinIdleAge:   agentStateGrace,
	}
	var guidedStatePtr *guidedCleanState
	if experience == cleanExperienceGuided {
		guidedState.Reason = reason
		guidedStatePtr = &guidedState
		// Guided policy owns active worktree selection. JSON mode accepts its
		// deterministic defaults without opening either guided prompt.
		opts.IncludeActiveWorktrees = false
	}

	targets := cleaner.Filter(result.Worktrees, opts)
	targets, physicalOwnerProtections := applyPhysicalWorktreeOwnerSafety(
		result.Worktrees,
		targets,
		opts.IncludeActiveWorktrees,
	)
	targets = filterExistingTargets(targets)
	targets, scanEvidenceProtections := filterTargetsWithoutScanEvidence(targets)
	targets = normalizeCleanTargets(targets)
	targets, gitSafetyProtections := filterGitUnsafeActiveWorktreeTargets(ctx, targets)
	classicProtections := mergeCleanAuditProtections(
		physicalOwnerProtections,
		scanEvidenceProtections,
		gitSafetyProtections,
	)
	logicalInputs := cleanupOverlapLogicalInputsForAudit(
		result.Worktrees,
		opts,
		classicProtections,
	)
	overlapSelection, err := applyCleanupOverlapSafetyWithRows(
		ctx,
		overlapSafety,
		targets,
		logicalInputs,
	)
	if err != nil {
		failCleanJSON("cleanup overlap safety preparation failed")
	}

	auditProtections := mergeCleanAuditProtections(classicProtections, overlapSelection.Protections)
	audit := buildPhysicalCleanAuditWithLogicalInputs(
		result.Worktrees,
		overlapSelection.Components,
		overlapSelection.Targets,
		opts,
		len(scanner.DefaultScanner.Providers),
		source,
		auditProtections,
		logicalInputs,
	)
	document, err := buildCleanJSONPlan(
		ctx,
		result,
		source,
		opts,
		guidedStatePtr,
		overlapSelection.Targets,
		auditProtections,
		audit,
	)
	if err != nil {
		failCleanJSON("cleanup plan projection failed")
	}
	if cleanDryRun {
		if err := encodeCleanJSON(os.Stdout, document); err != nil {
			failCleanJSON("cleanup plan encoding failed")
		}
		return
	}

	plan, err := unifiedCleanupPlanForClean(
		ctx,
		guidedStatePtr,
		overlapSelection.Targets,
		cleanupPlanEvidence(result, source, time.Now()),
	)
	if err != nil {
		failCleanJSON("cleanup plan preparation failed")
	}
	selected := plan.SelectedPhysicalTargets()
	if guidedStatePtr != nil {
		logicalInputs = applyGuidedPolicyReasons(logicalInputs, *guidedStatePtr)
	}
	executionSelection, err := applyCleanupOverlapSafetyWithRows(
		ctx,
		overlapSafety,
		selected,
		logicalInputs,
	)
	if err != nil {
		failCleanJSON("cleanup execution safety preparation failed")
	}
	prepared := prepareCleanExecutionWithOptions(ctx, executionSelection, overlapSafety, opts)
	components := buildCleanJSONSnapshotComponents(
		plan,
		audit.Components,
		result.Worktrees,
		auditProtections,
	)
	receipt, executionErr := executeCleanJSONReceipt(
		ctx,
		document,
		components,
		plan,
		prepared,
		cleanForce,
		cleanInteractive,
	)
	if err := encodeCleanJSONReceipt(os.Stdout, receipt); err != nil {
		failCleanJSON("cleanup receipt encoding failed")
	}
	if executionErr != nil || receipt.Status != cleanJSONReceiptSucceeded {
		fmt.Fprintln(os.Stderr, "error: cleanup execution did not succeed")
		os.Exit(1)
	}
}

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
	candidates := cleanJSONPlanCandidates(guidedState, classicTargets)
	plan, err := BuildUnifiedCleanupPlan(ctx, candidates, evidence)
	if err != nil {
		return cleanJSONPlan{}, err
	}
	components := buildCleanJSONSnapshotComponents(
		plan,
		audit.Components,
		result.Worktrees,
		protections,
	)
	return cleanJSONPlan{
		SchemaVersion:   cleanJSONSchemaVersion,
		DocumentType:    "clean_plan",
		Mode:            "dry_run",
		PathsIncluded:   cleanIncludePaths,
		Evidence:        cleanJSONEvidenceFor(source, evidence),
		Policy:          cleanJSONPolicyFor(opts, guidedState),
		Totals:          cleanJSONTotalsFor(components),
		PhysicalTargets: cleanJSONPhysicalTargetsFor(components),
		Rows:            cleanJSONRowsFor(components),
	}, nil
}

func encodeCleanJSON(output io.Writer, document cleanJSONPlan) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

func cleanJSONPlanCandidates(
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
) []CleanupPlanCandidate {
	classicTargets = normalizeCleanTargets(classicTargets)
	candidates := make([]CleanupPlanCandidate, 0, guidedCandidateCount(guidedState)+len(classicTargets))
	if guidedState != nil {
		candidates = append(candidates, guidedCleanupPlanCandidates(*guidedState)...)
	}
	candidates = append(candidates, ClassicCleanupPlanCandidates(classicTargets)...)

	return candidates
}

func cleanJSONReasonCodeForEligibility(reason cleaner.EligibilityReason) string {
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
	case cleaner.EligibilityReasonEligible:
		return "eligible"
	default:
		return "policy_decision"
	}
}

func cleanJSONReasonCodeForAuditReason(reason cleanAuditReason) string {
	switch reason {
	case cleanReasonFiltered:
		return "filtered"
	case cleanReasonRisky:
		return "risky_requires_opt_in"
	case cleanReasonActiveWorktree:
		return "active_worktree"
	case cleanReasonAge:
		return "minimum_age"
	case cleanReasonAgentStateLive:
		return "agent_state_live"
	case cleanReasonAgentStateUndetermined:
		return "agent_state_undetermined"
	case cleanReasonAgentStateMinIdleAge:
		return "agent_state_min_idle_age"
	case cleanReasonMissingPath:
		return "missing_path"
	case cleanReasonDuplicatePath:
		return "duplicate_path"
	case cleanReasonNestedTarget:
		return "nested_target"
	case cleanReasonOverlapTarget:
		return "overlap_target"
	case cleanReasonProtectedAgentStateAncestor:
		return "protected_agent_state_ancestor"
	case cleanReasonProtectedAgentStateDescendant:
		return "protected_agent_state_descendant"
	case cleanReasonAmbiguousOverlapIdentity:
		return "ambiguous_overlap_identity"
	case cleanReasonCommandOverlap:
		return "command_overlap"
	case cleanReasonNestedRevalidation:
		return "nested_revalidation"
	case cleanReasonNestedRevalidationRequired:
		return "nested_revalidation_required"
	case cleanReasonScanEvidenceUnavailable:
		return "scan_evidence_unavailable"
	case cleanReasonEligible:
		return "eligible"
	case cleanAuditReason(gitProtectionDirtyFiles):
		return "git_dirty_files"
	case cleanAuditReason(gitProtectionGitStatusUnavailable):
		return "git_evidence_unavailable"
	case cleanAuditReason(gitProtectionUpstreamComparisonUnavailable):
		return "git_upstream_unavailable"
	case cleanAuditReason(gitProtectionUnpushedCommits):
		return "git_unpushed_commits"
	default:
		return cleanJSONReasonCode(string(reason))
	}
}

func buildCleanJSONSnapshotComponents(
	plan UnifiedCleanupPlan,
	auditComponents []cleanupOverlapComponent,
	inventory []types.DebrisInfo,
	protections map[string]cleanAuditReason,
) []cleanJSONSnapshotComponent {
	components := make([]cleanJSONSnapshotComponent, 0, len(plan.Components)+len(auditComponents))
	componentIndexes := make(map[string]int, len(plan.Components))
	for _, component := range plan.Components {
		decision := cleanJSONDecisionForPlanSelection(component.Selection)
		componentIndexes[component.Key] = len(components)
		components = append(components, cleanJSONSnapshotComponent{
			Key:      component.Key,
			Owner:    component.Owner,
			Decision: decision,
			Rows:     []cleanJSONSnapshotRow{},
		})
	}

	planRowsRemaining := make(map[string]int, len(plan.Rows))
	for _, row := range plan.Rows {
		planRowsRemaining[cleanJSONRowIdentityKey(row.Item)]++
		componentIndex, ok := componentIndexes[row.OwnerKey]
		if !ok {
			continue
		}
		policyDecision := cleanJSONPolicyDecisionForPlanRow(row)
		reasons := cleanJSONPlanRowReasonCodes(row)
		if cleanJSONNeedsProtectedOverlapMarker(
			components[componentIndex].Decision,
			policyDecision,
			string(row.Relation),
		) {
			reasons = append(reasons, "protected_overlap")
		}
		components[componentIndex].Rows = append(components[componentIndex].Rows, cleanJSONSnapshotRow{
			Item:           row.Item,
			Relation:       string(row.Relation),
			PolicyDecision: policyDecision,
			Decision:       components[componentIndex].Decision,
			ReasonCodes:    reasons,
			SortKey:        cleanJSONSnapshotRowSortKey(row.Item, string(row.Relation), len(components[componentIndex].Rows)),
		})
	}

	for _, auditComponent := range auditComponents {
		componentIndex, matched := cleanJSONPlanComponentForPath(auditComponent.CanonicalPath, plan.Components)
		if matched {
			for _, row := range auditComponent.LogicalRows {
				key := cleanJSONRowIdentityKey(row.Item)
				if planRowsRemaining[key] > 0 {
					planRowsRemaining[key]--
					continue
				}
				appendCleanJSONAuditRow(
					&components[componentIndex],
					row,
					auditComponent,
					protections,
				)
			}
			continue
		}

		component := cleanJSONSnapshotComponent{
			Key:      auditComponent.CanonicalPath,
			Owner:    auditComponent.Owner,
			Decision: cleanJSONDecisionForAuditComponent(auditComponent, protections),
			Rows:     []cleanJSONSnapshotRow{},
		}
		for _, row := range auditComponent.LogicalRows {
			key := cleanJSONRowIdentityKey(row.Item)
			if planRowsRemaining[key] > 0 {
				planRowsRemaining[key]--
				continue
			}
			appendCleanJSONAuditRow(
				&component,
				row,
				auditComponent,
				protections,
			)
		}
		components = append(components, component)
	}

	// A scanner row normally appears in cleanAudit.Components. Keep the
	// projection total and row accounting defensive for synthetic/unit inputs.
	assigned := make(map[string]int)
	for _, component := range components {
		for _, row := range component.Rows {
			assigned[cleanJSONRowIdentityKey(row.Item)]++
		}
	}
	unassigned := make([]types.DebrisInfo, 0)
	for _, item := range inventory {
		key := cleanJSONRowIdentityKey(item)
		if assigned[key] >= 1 {
			assigned[key]--
			continue
		}
		componentIndex, matched := cleanJSONPlanComponentForPath(item.Path, plan.Components)
		if matched {
			appendCleanJSONInventoryRow(
				&components[componentIndex],
				item,
				protections,
			)
			continue
		}
		unassigned = append(unassigned, item)
	}
	if len(unassigned) > 0 {
		fallbackComponents, _ := cleanAuditPhysicalComponents(unassigned, nil)
		for _, fallback := range fallbackComponents {
			component := cleanJSONSnapshotComponent{
				Key:      fallback.CanonicalPath,
				Owner:    fallback.Owner,
				Decision: cleanJSONDecisionForAuditComponent(fallback, protections),
				Rows:     []cleanJSONSnapshotRow{},
			}
			for _, row := range fallback.LogicalRows {
				appendCleanJSONAuditRow(
					&component,
					row,
					fallback,
					protections,
				)
			}
			components = append(components, component)
		}
	}

	for i := range components {
		sort.SliceStable(components[i].Rows, func(left, right int) bool {
			return components[i].Rows[left].SortKey < components[i].Rows[right].SortKey
		})
		components[i].Rows = ensureCleanJSONOwnerRow(components[i])
	}
	sort.SliceStable(components, func(i, j int) bool {
		left := strings.Join([]string{components[i].Key, cleanTargetStableKey(components[i].Owner)}, "\x00")
		right := strings.Join([]string{components[j].Key, cleanTargetStableKey(components[j].Owner)}, "\x00")
		return left < right
	})
	assignCleanJSONAccountingBytes(components)
	return components
}

// assignCleanJSONAccountingBytes gives each action component a disjoint share
// of its containing on-disk tree. B1 safety intentionally keeps an unselected
// owner and a selected nested target as separate mutation owners; their raw
// directory-size estimates therefore overlap. The public plan charges child
// subtrees first (in canonical-path order) and assigns each owner only its
// remaining exclusive share. A fully covered owner deterministically receives
// zero bytes while retaining its decision and action identity.
func assignCleanJSONAccountingBytes(components []cleanJSONSnapshotComponent) {
	parents := make([]int, len(components))
	children := make([][]int, len(components))
	for i := range parents {
		parents[i] = -1
	}
	for child := range components {
		parentDepth := -1
		for candidate := range components {
			if candidate == child || !cleanTargetContains(components[candidate].Key, components[child].Key) {
				continue
			}
			depth := cleanTargetPathDepth(components[candidate].Key)
			if depth > parentDepth {
				parents[child] = candidate
				parentDepth = depth
			}
		}
		if parents[child] >= 0 {
			children[parents[child]] = append(children[parents[child]], child)
		}
	}

	var allocate func(int, int64)
	allocate = func(index int, budget int64) {
		if budget < 0 {
			budget = 0
		}
		ownerBytes := components[index].Owner.Size
		if ownerBytes < 0 {
			ownerBytes = 0
		}
		if budget > ownerBytes {
			budget = ownerBytes
		}
		remaining := budget
		for _, child := range children[index] {
			childBudget := components[child].Owner.Size
			if childBudget < 0 {
				childBudget = 0
			}
			if childBudget > remaining {
				childBudget = remaining
			}
			allocate(child, childBudget)
			remaining -= childBudget
		}
		components[index].AccountingBytes = remaining
	}
	for index := range components {
		if parents[index] >= 0 {
			continue
		}
		allocate(index, components[index].Owner.Size)
	}
}

func cleanJSONPlanComponentForPath(path string, components []CleanupPhysicalComponent) (int, bool) {
	canonical, ok := cleanTargetPathKey(path)
	if !ok {
		return 0, false
	}
	for i, component := range components {
		if component.CanonicalPath == canonical {
			return i, true
		}
	}
	best := -1
	bestDepth := -1
	for i, component := range components {
		if cleanTargetContains(component.CanonicalPath, canonical) {
			depth := cleanTargetPathDepth(component.CanonicalPath)
			if depth > bestDepth {
				best = i
				bestDepth = depth
			}
		}
	}
	if best >= 0 {
		return best, true
	}
	best = -1
	bestDepth = int(^uint(0) >> 1)
	for i, component := range components {
		if cleanTargetContains(canonical, component.CanonicalPath) {
			depth := cleanTargetPathDepth(component.CanonicalPath)
			if depth < bestDepth {
				best = i
				bestDepth = depth
			}
		}
	}
	return best, best >= 0
}

func appendCleanJSONAuditRow(
	component *cleanJSONSnapshotComponent,
	row cleanupOverlapLogicalRow,
	auditComponent cleanupOverlapComponent,
	protections map[string]cleanAuditReason,
) {
	info := cleanJSONPolicyForAuditRow(row, auditComponent, protections)
	relation := cleanJSONRelationForAuditRow(row, component.Owner)
	reasons := append([]string(nil), info.ReasonCodes...)
	if cleanJSONNeedsProtectedOverlapMarker(component.Decision, info.Decision, relation) {
		reasons = append(reasons, "protected_overlap")
	}
	component.Rows = append(component.Rows, cleanJSONSnapshotRow{
		Item:           row.Item,
		Relation:       relation,
		PolicyDecision: info.Decision,
		Decision:       component.Decision,
		ReasonCodes:    uniqueCleanJSONReasonCodes(reasons),
		SortKey:        cleanJSONSnapshotRowSortKey(row.Item, relation, len(component.Rows)),
	})
}

func appendCleanJSONInventoryRow(
	component *cleanJSONSnapshotComponent,
	item types.DebrisInfo,
	protections map[string]cleanAuditReason,
) {
	info := cleanJSONPolicyForInventoryItem(item, protections)
	relation := cleanJSONRelationForInventoryItem(item, component)
	reasons := append([]string(nil), info.ReasonCodes...)
	if cleanJSONNeedsProtectedOverlapMarker(component.Decision, info.Decision, relation) {
		reasons = append(reasons, "protected_overlap")
	}
	component.Rows = append(component.Rows, cleanJSONSnapshotRow{
		Item:           item,
		Relation:       relation,
		PolicyDecision: info.Decision,
		Decision:       component.Decision,
		ReasonCodes:    uniqueCleanJSONReasonCodes(reasons),
		SortKey:        cleanJSONSnapshotRowSortKey(item, relation, len(component.Rows)),
	})
}

func cleanJSONNeedsProtectedOverlapMarker(componentDecision, policyDecision, relation string) bool {
	if relation == string(CleanupPlanRelationOwner) {
		return false
	}
	if componentDecision == cleanJSONDecisionProtected && policyDecision != cleanJSONPolicyProtected {
		return true
	}
	return componentDecision == cleanJSONDecisionSelected &&
		(policyDecision == cleanJSONPolicyProtected || policyDecision == cleanJSONPolicyReviewable)
}

func cleanJSONPolicyForInventoryItem(
	item types.DebrisInfo,
	protections map[string]cleanAuditReason,
) cleanJSONPolicyInfo {
	if reason := protections[cleanAuditItemKey(item)]; reason != "" {
		return cleanJSONPolicyInfo{
			Decision:    cleanJSONPolicyProtected,
			ReasonCodes: []string{cleanJSONReasonCodeForAuditReason(reason)},
		}
	}
	return cleanJSONPolicyInfo{
		Decision:    cleanJSONPolicySkipped,
		ReasonCodes: []string{"policy_decision"},
	}
}

func cleanJSONPolicyForAuditRow(
	row cleanupOverlapLogicalRow,
	component cleanupOverlapComponent,
	protections map[string]cleanAuditReason,
) cleanJSONPolicyInfo {
	if reason := protections[cleanAuditItemKey(row.Item)]; reason != "" {
		return cleanJSONPolicyInfo{
			Decision:    cleanJSONPolicyProtected,
			ReasonCodes: []string{cleanJSONReasonCodeForAuditReason(reason)},
		}
	}
	if component.Refusal != nil {
		return cleanJSONPolicyInfo{
			Decision: cleanJSONPolicyProtected,
			ReasonCodes: []string{
				cleanJSONReasonCodeForAuditReason(cleanAuditReasonForOverlapSafety(component.Refusal.Reason)),
			},
		}
	}
	decision := row.PolicyDecision
	if decision == "" {
		decision = cleanJSONPolicySkipped
	}
	return cleanJSONPolicyInfo{
		Decision:    decision,
		ReasonCodes: uniqueCleanJSONReasonCodes(row.ReasonCodes),
	}
}

// cleanJSONDecisionForAuditComponent preserves standalone owner safety and
// reviewable policy decisions. A protected inventory row attached to a
// separately selected nested component remains evidence on that selected
// component: upgrading it into a locked plan candidate would reintroduce the
// B1 containment lockout.
func cleanJSONDecisionForAuditComponent(
	component cleanupOverlapComponent,
	protections map[string]cleanAuditReason,
) string {
	if component.Refusal != nil || protections[cleanAuditItemKey(component.Owner)] != "" {
		return cleanJSONDecisionProtected
	}
	ownerKey := cleanAuditItemKey(component.Owner)
	for _, row := range component.LogicalRows {
		if cleanAuditItemKey(row.Item) != ownerKey {
			continue
		}
		ownerPolicy := cleanJSONPolicyForAuditRow(row, component, protections).Decision
		switch ownerPolicy {
		case cleanJSONPolicyProtected:
			return cleanJSONDecisionProtected
		case cleanJSONPolicyReviewable:
			return cleanJSONDecisionReviewable
		}
	}
	return cleanJSONDecisionSkipped
}

func cleanJSONRelationForAuditRow(row cleanupOverlapLogicalRow, owner types.DebrisInfo) string {
	switch row.Relation {
	case cleanupOverlapOwner:
		return string(CleanupPlanRelationOwner)
	case cleanupOverlapAncestor:
		return string(CleanupPlanRelationAncestor)
	case cleanupOverlapExact:
		return string(CleanupPlanRelationExact)
	case cleanupOverlapDescendant:
		return string(CleanupPlanRelationNested)
	}
	ownerPath, ownerOK := cleanTargetPathKey(owner.Path)
	rowPath, rowOK := cleanTargetPathKey(row.Item.Path)
	if ownerOK && rowOK && ownerPath == rowPath {
		return string(CleanupPlanRelationExact)
	}
	return string(CleanupPlanRelationNested)
}

func cleanJSONRelationForInventoryItem(item types.DebrisInfo, component *cleanJSONSnapshotComponent) string {
	ownerPath, ownerOK := cleanTargetPathKey(component.Owner.Path)
	itemPath, itemOK := cleanTargetPathKey(item.Path)
	if ownerOK && itemOK {
		switch {
		case ownerPath == itemPath:
			if len(component.Rows) == 0 {
				return string(CleanupPlanRelationOwner)
			}
			return string(CleanupPlanRelationExact)
		case cleanTargetContains(itemPath, ownerPath):
			return string(CleanupPlanRelationAncestor)
		case cleanTargetContains(ownerPath, itemPath):
			return string(CleanupPlanRelationNested)
		}
	}
	if len(component.Rows) == 0 {
		return string(CleanupPlanRelationOwner)
	}
	return string(CleanupPlanRelationNested)
}

func ensureCleanJSONOwnerRow(component cleanJSONSnapshotComponent) []cleanJSONSnapshotRow {
	rows := component.Rows
	for _, row := range rows {
		if row.Relation == string(CleanupPlanRelationOwner) {
			return rows
		}
	}
	if len(rows) == 0 {
		return rows
	}
	rows[0].Relation = string(CleanupPlanRelationOwner)
	return rows
}

func cleanJSONDecisionForPlanSelection(selection CleanupPlanSelection) string {
	switch selection {
	case CleanupPlanSelected:
		return cleanJSONDecisionSelected
	case CleanupPlanLocked:
		return cleanJSONDecisionProtected
	default:
		return cleanJSONDecisionReviewable
	}
}

func cleanJSONPolicyDecisionForPlanRow(row CleanupPlanRow) string {
	if row.PolicyDecision != "" {
		return string(row.PolicyDecision)
	}
	selection := row.PolicySelection
	if selection == "" {
		selection = row.Selection
	}
	switch selection {
	case CleanupPlanLocked:
		return cleanJSONPolicyProtected
	case CleanupPlanSelected:
		return cleanJSONPolicyEligible
	default:
		return cleanJSONPolicyReviewable
	}
}

func cleanJSONPlanRowReasonCodes(row CleanupPlanRow) []string {
	codes := make([]string, 0, len(row.Reasons))
	for _, reason := range row.Reasons {
		codes = append(codes, cleanJSONReasonCode(string(reason.Code)))
	}
	return uniqueCleanJSONReasonCodes(codes)
}

func cleanJSONSnapshotRowSortKey(item types.DebrisInfo, relation string, ordinal int) string {
	relationRank := "2"
	switch relation {
	case string(CleanupPlanRelationOwner):
		relationRank = "0"
	case string(CleanupPlanRelationExact):
		relationRank = "1"
	}
	return strings.Join([]string{
		relationRank,
		cleanTargetStableKey(item),
		fmt.Sprintf("%09d", ordinal),
	}, "\x00")
}

func cleanJSONEvidenceFor(source scanSource, evidence CleanupPlanEvidence) cleanJSONEvidence {
	sourceName := string(source.Kind)
	if sourceName == "" {
		sourceName = string(scanSourceLive)
	}
	observedAt := evidence.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	return cleanJSONEvidence{
		Complete:   len(evidence.ProviderErrors) == 0,
		Source:     sourceName,
		ObservedAt: observedAt.Format(time.RFC3339Nano),
	}
}

func cleanJSONPolicyFor(opts types.PruneOptions, guidedState *guidedCleanState) cleanJSONPolicy {
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
	policy := cleanJSONPolicy{
		MinimumAge:             cleanAgeDisplay(opts.Age),
		AgentStateGrace:        cleanAgeDisplay(opts.AgentStateMinIdleAge),
		Categories:             categories,
		Tools:                  tools,
		Risky:                  opts.Risky,
		IncludeActiveWorktrees: opts.IncludeActiveWorktrees,
	}
	if guidedState != nil {
		policy.GuidedMinIdleAge = cleanAgeDisplay(fillCleanupPolicy(guidedState.Policy).MinIdleAge)
	}
	return policy
}

func cleanJSONTotalsFor(components []cleanJSONSnapshotComponent) cleanJSONTotals {
	totals := cleanJSONTotals{}
	for _, component := range components {
		totals.PhysicalTargets++
		totals.PhysicalBytes += component.AccountingBytes
		totals.VisibleRows += len(component.Rows)
		switch component.Decision {
		case cleanJSONDecisionSelected:
			totals.Selected++
			totals.SelectedBytes += component.AccountingBytes
		case cleanJSONDecisionReviewable:
			totals.Reviewable++
			totals.ReviewableBytes += component.AccountingBytes
		case cleanJSONDecisionProtected:
			totals.Protected++
			totals.ProtectedBytes += component.AccountingBytes
		case cleanJSONDecisionSkipped:
			totals.Skipped++
			totals.SkippedBytes += component.AccountingBytes
		}
	}
	return totals
}

func cleanJSONPhysicalTargetsFor(components []cleanJSONSnapshotComponent) []cleanJSONPhysicalTarget {
	targets := make([]cleanJSONPhysicalTarget, 0, len(components))
	for i, component := range components {
		target := cleanJSONPhysicalTarget{
			ID:          fmt.Sprintf("target-%d", i+1),
			Decision:    component.Decision,
			Bytes:       component.AccountingBytes,
			Category:    string(component.Owner.Category),
			Tool:        string(component.Owner.Tool),
			CleanupKind: string(cleanupKind(component.Owner)),
		}
		if cleanIncludePaths {
			path := component.Owner.Path
			target.Path = &path
		}
		targets = append(targets, target)
	}
	return targets
}

func cleanJSONRowsFor(components []cleanJSONSnapshotComponent) []cleanJSONRow {
	rows := make([]cleanJSONRow, 0)
	for componentIndex, component := range components {
		physicalTargetID := fmt.Sprintf("target-%d", componentIndex+1)
		for _, snapshotRow := range component.Rows {
			row := cleanJSONRow{
				ID:               fmt.Sprintf("row-%d", len(rows)+1),
				PhysicalTargetID: physicalTargetID,
				Relation:         snapshotRow.Relation,
				PolicyDecision:   snapshotRow.PolicyDecision,
				Decision:         snapshotRow.Decision,
				Category:         string(snapshotRow.Item.Category),
				Tool:             string(snapshotRow.Item.Tool),
				ReasonCodes:      append([]string{}, snapshotRow.ReasonCodes...),
			}
			if row.ReasonCodes == nil {
				row.ReasonCodes = []string{}
			}
			if cleanIncludePaths {
				path := snapshotRow.Item.Path
				project := snapshotRow.Item.Project
				command := append([]string{}, snapshotRow.Item.CleanupCommand...)
				if command == nil {
					command = []string{}
				}
				row.Path = &path
				row.Project = &project
				row.CleanupCommand = &command
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func cleanJSONReasonCode(code string) string {
	switch code {
	case "classic_eligible", "agent_state_orphaned", "contains_locked_target", "overlaps_locked_target", "worktree_policy_decision",
		"current_working_directory", "git_dirty_or_untracked", "git_evidence_unavailable", "git_detached_head_unreferenced", "activity_evidence_unavailable", "recent_activity", "retained_per_repository", "younger_than_min_idle_age", "below_min_size", "cleanup_recommended", "git_attached_local_branch", "git_detached_head_reachable",
		"filtered", "risky_requires_opt_in", "active_worktree", "worktree_requires_review", "minimum_age", "agent_state_live", "agent_state_undetermined", "agent_state_min_idle_age", "eligible", "missing_path", "duplicate_path", "nested_target", "overlap_target", "protected_agent_state_ancestor", "protected_agent_state_descendant", "ambiguous_overlap_identity", "command_overlap", "nested_revalidation", "nested_revalidation_required", "scan_evidence_unavailable", "protected_overlap", "not_selected", "policy_protected", "policy_decision", "git_dirty_files", "git_upstream_unavailable", "git_unpushed_commits",
		"removed", "partial_failure", "execution_failed", "cancelled", "physical_owner_present", "command_fallback_path_removal", "safety_refused", "execution_set_mismatch", "plan_validation_failed", "cancelled_before_execution", "cancelled_after_confirmation", "cancelled_after_execution", "cancelled_during_confirmation", "confirmation_cancelled", "invalid_confirmation", "not_confirmed", "execution_not_recorded", "execution_state":
		return code
	default:
		return "policy_decision"
	}
}

// cleanJSONRowIdentityKey identifies one logical JSON evidence row by its
// stable fields and canonical path. When canonicalization cannot resolve a
// path, its cleaned raw spelling remains a safe, deterministic fallback.
func cleanJSONRowIdentityKey(item types.DebrisInfo) string {
	pathKey := strings.TrimSpace(item.Path)
	if canonical, ok := cleanTargetPathKey(item.Path); ok {
		pathKey = canonical
	} else if pathKey != "" {
		pathKey = cleanTargetRawPathKey(pathKey)
	} else {
		pathKey = "<empty-path>"
	}
	return strings.Join([]string{
		string(item.Category),
		string(item.Tool),
		item.ID,
		pathKey,
	}, "\x00")
}

func uniqueCleanJSONReasonCodes(codes []string) []string {
	seen := make(map[string]bool, len(codes))
	unique := make([]string, 0, len(codes))
	for _, code := range codes {
		code = cleanJSONReasonCode(code)
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
