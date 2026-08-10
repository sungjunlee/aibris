package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	errPartialCleanupPlanEvidence = errors.New("cleanup plan evidence is partial")
	errStaleCleanupPlanEvidence   = errors.New("cleanup plan evidence is stale")
)

// CleanupPlanSelection is the user-visible and executable state of a plan row.
// Locked rows are never selectable, including when --force is used later.
type CleanupPlanSelection string

const (
	CleanupPlanSelected   CleanupPlanSelection = "selected"
	CleanupPlanUnselected CleanupPlanSelection = "unselected"
	CleanupPlanLocked     CleanupPlanSelection = "locked"
)

// CleanupPlanPolicyDecision is the source policy classification, independent
// of the user's accepted selection state.
type CleanupPlanPolicyDecision string

const (
	CleanupPlanPolicyEligible    CleanupPlanPolicyDecision = "eligible"
	CleanupPlanPolicyRecommended CleanupPlanPolicyDecision = "recommended"
	CleanupPlanPolicyReviewable  CleanupPlanPolicyDecision = "reviewable"
	CleanupPlanPolicyProtected   CleanupPlanPolicyDecision = "protected"
	CleanupPlanPolicySkipped     CleanupPlanPolicyDecision = "skipped"
)

type CleanupPlanReasonCode string

const (
	CleanupPlanReasonClassicEligible        CleanupPlanReasonCode = "classic_eligible"
	CleanupPlanReasonAgentStateOrphaned     CleanupPlanReasonCode = "agent_state_orphaned"
	CleanupPlanReasonAgentStateGracePeriod  CleanupPlanReasonCode = "agent_state_grace_period"
	CleanupPlanReasonContainsLockedTarget   CleanupPlanReasonCode = "contains_locked_target"
	CleanupPlanReasonOverlapsLockedTarget   CleanupPlanReasonCode = "overlaps_locked_target"
	CleanupPlanReasonWorktreePolicyDecision CleanupPlanReasonCode = "worktree_policy_decision"
)

type CleanupPlanReason struct {
	Code        CleanupPlanReasonCode
	Description string
}

// CleanupPlanCandidate is the policy-neutral input boundary. Classic filtering
// and guided worktree policy each adapt their existing decisions into this
// shape instead of reimplementing policy in the unified plan.
type CleanupPlanCandidate struct {
	RowKey         string
	Item           types.DebrisInfo
	PolicyDecision CleanupPlanPolicyDecision
	Selection      CleanupPlanSelection
	Reasons        []CleanupPlanReason
}

// CleanupPlanEvidence records whether the scan can authorize a later
// execution. MaxAge zero means the caller has not imposed an expiry.
type CleanupPlanEvidence struct {
	ObservedAt     time.Time
	MaxAge         time.Duration
	ProviderErrors []types.ScanProviderError
}

// CleanupPlanRow is one visible policy decision. More than one row may refer
// to the same physical target.
type CleanupPlanRow struct {
	Key             string
	TargetKey       string
	OwnerKey        string
	CanonicalPath   string
	Relation        CleanupPlanRelation
	Item            types.DebrisInfo
	PolicyDecision  CleanupPlanPolicyDecision
	PolicySelection CleanupPlanSelection
	Selection       CleanupPlanSelection
	Reasons         []CleanupPlanReason
	PhysicalBytes   int64
}

// CleanupPlanRelation describes how a visible logical row relates to the
// physical target that owns its bytes. The JSON contract includes owner,
// exact, nested, and ancestor.
type CleanupPlanRelation string

const (
	CleanupPlanRelationOwner    CleanupPlanRelation = "owner"
	CleanupPlanRelationExact    CleanupPlanRelation = "exact"
	CleanupPlanRelationNested   CleanupPlanRelation = "nested"
	CleanupPlanRelationAncestor CleanupPlanRelation = "ancestor"
)

// CleanupPhysicalTarget is one exact canonical path and retains every
// discovery row at that path. Component ownership is recorded separately so
// exact duplicates remain distinguishable without adding physical bytes.
type CleanupPhysicalTarget struct {
	Key             string
	OwnerKey        string
	Item            types.DebrisInfo
	RowKeys         []string
	PolicySelection CleanupPlanSelection
	Selection       CleanupPlanSelection
}

// CleanupPhysicalComponent is one containment-connected on-disk unit. Owner is
// the outermost canonical target and is the only source of physical bytes.
type CleanupPhysicalComponent struct {
	Key            string
	CanonicalPath  string
	OwnerTargetKey string
	Owner          types.DebrisInfo
	TargetKeys     []string
	RowKeys        []string
	Selection      CleanupPlanSelection
}

type CleanupPlanTotals struct {
	VisibleRows       int
	PhysicalTargets   int
	PhysicalBytes     int64
	EligibleTargets   int
	EligibleBytes     int64
	SelectedTargets   int
	SelectedBytes     int64
	ReviewableTargets int
	ReviewableBytes   int64
	UnselectedRows    int
	HardLockedRows    int
	HardLockedTargets int
	HardLockedBytes   int64
}

// UnifiedCleanupPlan is the shared, renderer-independent state for mixed
// cleanup review and execution.
type UnifiedCleanupPlan struct {
	Rows       []CleanupPlanRow
	Targets    []CleanupPhysicalTarget
	Components []CleanupPhysicalComponent
	Evidence   CleanupPlanEvidence
}

type cleanupPlanTargetGroup struct {
	key        string
	candidates []CleanupPlanCandidate
}

// BuildUnifiedCleanupPlan normalizes visible policy decisions into exact
// physical targets. A hard lock in either containment direction dominates the
// complete component.
func BuildUnifiedCleanupPlan(ctx context.Context, candidates []CleanupPlanCandidate, evidence CleanupPlanEvidence) (UnifiedCleanupPlan, error) {
	if err := ctx.Err(); err != nil {
		return UnifiedCleanupPlan{}, err
	}

	ordered := append([]CleanupPlanCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return cleanupPlanCandidateStableKey(ordered[i]) < cleanupPlanCandidateStableKey(ordered[j])
	})

	rowKeys := make(map[string]bool, len(ordered))
	generatedRowKeys := make(map[string]int, len(ordered))
	groupsByPath := make(map[string]*cleanupPlanTargetGroup, len(ordered))
	for _, candidate := range ordered {
		if err := ctx.Err(); err != nil {
			return UnifiedCleanupPlan{}, err
		}
		if !validCleanupPlanSelection(candidate.Selection) {
			return UnifiedCleanupPlan{}, fmt.Errorf("invalid cleanup plan selection %q", candidate.Selection)
		}
		path, ok := cleanTargetPathKey(candidate.Item.Path)
		if !ok {
			return UnifiedCleanupPlan{}, fmt.Errorf("cleanup plan row %q has no target path", candidate.RowKey)
		}
		if candidate.RowKey == "" {
			baseKey := cleanupPlanCandidateStableKey(candidate)
			generatedRowKeys[baseKey]++
			candidate.RowKey = fmt.Sprintf("%s#%d", baseKey, generatedRowKeys[baseKey])
		}
		if rowKeys[candidate.RowKey] {
			return UnifiedCleanupPlan{}, fmt.Errorf("duplicate cleanup plan row key %q", candidate.RowKey)
		}
		rowKeys[candidate.RowKey] = true
		group := groupsByPath[path]
		if group == nil {
			group = &cleanupPlanTargetGroup{key: path}
			groupsByPath[path] = group
		}
		group.candidates = append(group.candidates, candidate)
	}

	groupKeys := make([]string, 0, len(groupsByPath))
	for key := range groupsByPath {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)

	rows := make([]CleanupPlanRow, 0, len(ordered))
	targets := make([]CleanupPhysicalTarget, 0, len(groupKeys))
	for _, key := range groupKeys {
		if err := ctx.Err(); err != nil {
			return UnifiedCleanupPlan{}, err
		}
		group := groupsByPath[key]
		sort.SliceStable(group.candidates, func(i, j int) bool {
			return cleanupPlanCandidateStableKey(group.candidates[i]) < cleanupPlanCandidateStableKey(group.candidates[j])
		})
		selection := aggregateCleanupPlanSelection(group.candidates)
		item := cleanupPlanRepresentative(key, group.candidates)
		target := CleanupPhysicalTarget{
			Key:             key,
			Item:            item,
			PolicySelection: selection,
			Selection:       selection,
		}
		for _, candidate := range group.candidates {
			target.RowKeys = append(target.RowKeys, candidate.RowKey)
			rows = append(rows, CleanupPlanRow{
				Key:             candidate.RowKey,
				TargetKey:       key,
				CanonicalPath:   key,
				Item:            candidate.Item,
				PolicyDecision:  candidate.PolicyDecision,
				PolicySelection: candidate.Selection,
				Selection:       selection,
				Reasons:         append([]CleanupPlanReason(nil), candidate.Reasons...),
			})
		}
		targets = append(targets, target)
	}

	components := buildCleanupPhysicalComponents(rows, targets)
	sort.Slice(rows, func(i, j int) bool {
		return cleanupPlanRowStableKey(rows[i]) < cleanupPlanRowStableKey(rows[j])
	})
	evidence.ProviderErrors = sortedProviderErrors(evidence.ProviderErrors)
	return UnifiedCleanupPlan{
		Rows:       rows,
		Targets:    targets,
		Components: components,
		Evidence:   evidence,
	}, nil
}

// ClassicCleanupPlanCandidates adapts already-filtered classic targets.
func ClassicCleanupPlanCandidates(targets []types.DebrisInfo) []CleanupPlanCandidate {
	candidates := make([]CleanupPlanCandidate, 0, len(targets))
	for _, target := range targets {
		reason := CleanupPlanReason{
			Code:        CleanupPlanReasonClassicEligible,
			Description: "eligible under classic cleanup filters",
		}
		if target.Category == types.CategoryAgentState {
			reason = CleanupPlanReason{
				Code:        CleanupPlanReasonAgentStateOrphaned,
				Description: "recorded working directory is absent",
			}
		}
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:         "classic:" + cleanTargetStableKey(target),
			Item:           target,
			PolicyDecision: CleanupPlanPolicyEligible,
			Selection:      CleanupPlanSelected,
			Reasons:        []CleanupPlanReason{reason},
		})
	}
	return candidates
}

// ReviewableAgentStateCleanupPlanCandidates adapts orphaned agent-state
// entries still inside the grace period into reviewable plan rows. They are
// never default-selected but remain selectable in the unified review, unlike
// hard-locked protected entries. Eligible orphans are already carried by the
// classic candidates and are skipped here.
func ReviewableAgentStateCleanupPlanCandidates(items []types.DebrisInfo, observedAt time.Time) []CleanupPlanCandidate {
	var candidates []CleanupPlanCandidate
	for _, item := range items {
		if item.Category != types.CategoryAgentState ||
			item.Classification != types.EntryClassOrphaned {
			continue
		}
		eligible, reason := cleaner.EvaluateEligibility(item, types.PruneOptions{}, observedAt)
		if eligible || reason != cleaner.EligibilityReasonAgentStateReviewable {
			continue
		}
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:         "reviewable-agent-state:" + cleanTargetStableKey(item),
			Item:           item,
			PolicyDecision: CleanupPlanPolicyReviewable,
			Selection:      CleanupPlanUnselected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonAgentStateGracePeriod,
				Description: "orphaned agent-state younger than grace period; reviewable",
			}},
		})
	}
	return candidates
}

// WorktreeCleanupPlanCandidates adapts the existing deterministic worktree
// policy without duplicating its classification rules.
func WorktreeCleanupPlanCandidates(plan CleanupPlan, items []types.DebrisInfo) []CleanupPlanCandidate {
	candidates := make([]CleanupPlanCandidate, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		reasons := make([]CleanupPlanReason, 0, len(decision.Reasons))
		for _, reason := range decision.Reasons {
			description := reason.Description
			if description == "" {
				description = string(reason.Code)
			}
			reasons = append(reasons, CleanupPlanReason{
				Code:        CleanupPlanReasonCode(reason.Code),
				Description: description,
			})
		}
		if len(reasons) == 0 {
			reasons = append(reasons, CleanupPlanReason{
				Code:        CleanupPlanReasonWorktreePolicyDecision,
				Description: "worktree cleanup policy decision",
			})
		}
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:         "worktree:" + cleanupUnitStableKey(decision.Unit),
			Item:           guidedCleanupUnitItem(decision.Unit, items),
			PolicyDecision: cleanupPlanPolicyDecisionForClass(decision.Class),
			Selection:      cleanupPlanSelectionForDecision(decision.Class),
			Reasons:        reasons,
		})
	}
	return candidates
}

// ValidateForExecution rejects cancellation, partial scan evidence, and plans
// whose caller-defined evidence window has expired.
func (p UnifiedCleanupPlan) ValidateForExecution(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(p.Evidence.ProviderErrors) > 0 {
		return fmt.Errorf("%w: %d provider(s) failed", errPartialCleanupPlanEvidence, len(p.Evidence.ProviderErrors))
	}
	if p.Evidence.MaxAge > 0 &&
		(p.Evidence.ObservedAt.IsZero() || now.After(p.Evidence.ObservedAt.Add(p.Evidence.MaxAge))) {
		return errStaleCleanupPlanEvidence
	}
	return nil
}

// SelectedPhysicalTargets returns deterministic, overlap-normalized execution
// targets. It never returns locked or unselected targets.
func (p UnifiedCleanupPlan) SelectedPhysicalTargets() []types.DebrisInfo {
	selected := make([]types.DebrisInfo, 0, len(p.Components))
	for _, component := range p.Components {
		if component.Selection == CleanupPlanSelected {
			selected = append(selected, component.Owner)
		}
	}
	return selected
}

func (p UnifiedCleanupPlan) Totals() CleanupPlanTotals {
	totals := CleanupPlanTotals{VisibleRows: len(p.Rows)}
	for _, row := range p.Rows {
		switch row.Selection {
		case CleanupPlanUnselected:
			totals.UnselectedRows++
		case CleanupPlanLocked:
			totals.HardLockedRows++
		}
	}
	totals.PhysicalTargets = len(p.Components)
	for _, component := range p.Components {
		totals.PhysicalBytes += component.Owner.Size
		switch component.Selection {
		case CleanupPlanSelected:
			totals.EligibleTargets++
			totals.EligibleBytes += component.Owner.Size
			totals.SelectedTargets++
			totals.SelectedBytes += component.Owner.Size
		case CleanupPlanUnselected:
			totals.EligibleTargets++
			totals.EligibleBytes += component.Owner.Size
			totals.ReviewableTargets++
			totals.ReviewableBytes += component.Owner.Size
		case CleanupPlanLocked:
			totals.HardLockedTargets++
			totals.HardLockedBytes += component.Owner.Size
		}
	}
	return totals
}

func validCleanupPlanSelection(selection CleanupPlanSelection) bool {
	switch selection {
	case CleanupPlanSelected, CleanupPlanUnselected, CleanupPlanLocked:
		return true
	default:
		return false
	}
}

func aggregateCleanupPlanSelection(candidates []CleanupPlanCandidate) CleanupPlanSelection {
	selection := CleanupPlanUnselected
	for _, candidate := range candidates {
		switch candidate.Selection {
		case CleanupPlanLocked:
			return CleanupPlanLocked
		case CleanupPlanSelected:
			selection = CleanupPlanSelected
		}
	}
	return selection
}

func cleanupPlanRepresentative(canonicalPath string, candidates []CleanupPlanCandidate) types.DebrisInfo {
	item := candidates[0].Item
	hasActiveWorktree := isActiveWorktreeTarget(item)
	// Exact canonical aliases remain distinct raw mutation paths. A direct
	// physical candidate owns the component when available, and only duplicate
	// rows for that raw path may refine its byte estimate.
	rawSizes := map[string]int64{cleanTargetRawPathKey(item.Path): item.Size}
	for _, candidate := range candidates[1:] {
		hasActiveWorktree = hasActiveWorktree ||
			isActiveWorktreeTarget(candidate.Item)
		rawPath := cleanTargetRawPathKey(candidate.Item.Path)
		if candidate.Item.Size > rawSizes[rawPath] {
			rawSizes[rawPath] = candidate.Item.Size
		}
		if preferCleanTargetForCanonical(candidate.Item, item, canonicalPath) {
			item = candidate.Item
		}
	}
	if hasActiveWorktree && item.Category == types.CategoryWorktree {
		item.Status = types.WorktreeActive
	}
	item.Size = rawSizes[cleanTargetRawPathKey(item.Path)]
	return item
}

func cleanupPlanCandidateStableKey(candidate CleanupPlanCandidate) string {
	return strings.Join([]string{
		candidate.RowKey,
		cleanTargetStableKey(candidate.Item),
		string(candidate.Selection),
	}, "\x00")
}

func cleanupPlanSelectionForDecision(class DecisionClass) CleanupPlanSelection {
	switch class {
	case DecisionLocked:
		return CleanupPlanLocked
	case DecisionRecommended:
		return CleanupPlanSelected
	default:
		return CleanupPlanUnselected
	}
}

func cleanupPlanPolicyDecisionForClass(class DecisionClass) CleanupPlanPolicyDecision {
	switch class {
	case DecisionLocked:
		return CleanupPlanPolicyProtected
	case DecisionRecommended:
		return CleanupPlanPolicyRecommended
	case DecisionReviewable:
		return CleanupPlanPolicyReviewable
	default:
		return CleanupPlanPolicySkipped
	}
}

func buildCleanupPhysicalComponents(
	rows []CleanupPlanRow,
	targets []CleanupPhysicalTarget,
) []CleanupPhysicalComponent {
	ordered := make([]int, len(targets))
	for i := range targets {
		ordered[i] = i
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := targets[ordered[i]].Key
		right := targets[ordered[j]].Key
		leftDepth := cleanTargetPathDepth(left)
		rightDepth := cleanTargetPathDepth(right)
		if leftDepth == rightDepth {
			return left < right
		}
		return leftDepth < rightDepth
	})

	componentByOwner := make(map[string]*CleanupPhysicalComponent)
	for _, targetIndex := range ordered {
		target := &targets[targetIndex]
		ownerKey := target.Key
		for _, previousIndex := range ordered {
			previous := targets[previousIndex]
			if previous.Key == target.Key {
				break
			}
			// A nested target is absorbed into an ancestor's component only
			// when that ancestor is itself selected or locked. An unselected
			// owner (for example a kept reviewable worktree) must never be
			// promoted to selected by a nested selected row, and it must
			// never become the execution owner of that row; the nested target
			// stays its own physical owner instead.
			if cleanTargetContains(previous.Key, target.Key) &&
				previous.PolicySelection != CleanupPlanUnselected {
				ownerKey = previous.OwnerKey
				break
			}
		}
		target.OwnerKey = ownerKey
		component := componentByOwner[ownerKey]
		if component == nil {
			component = &CleanupPhysicalComponent{
				Key:            ownerKey,
				CanonicalPath:  ownerKey,
				OwnerTargetKey: target.Key,
				Owner:          target.Item,
				Selection:      CleanupPlanUnselected,
			}
			componentByOwner[ownerKey] = component
		}
		component.TargetKeys = append(component.TargetKeys, target.Key)
		component.RowKeys = append(component.RowKeys, target.RowKeys...)
		component.Selection = aggregateCleanupPlanComponentSelection(component.Selection, target.PolicySelection)
	}

	components := make([]CleanupPhysicalComponent, 0, len(componentByOwner))
	for _, component := range componentByOwner {
		sort.Strings(component.TargetKeys)
		sort.Strings(component.RowKeys)
		for i := range targets {
			if targets[i].OwnerKey == component.Key {
				targets[i].Selection = component.Selection
			}
		}
		for i := range rows {
			target := cleanupPhysicalTargetByKey(targets, rows[i].TargetKey)
			if target == nil || target.OwnerKey != component.Key {
				continue
			}
			rows[i].OwnerKey = component.Key
			rows[i].Selection = component.Selection
			ownerTarget := cleanupPhysicalTargetByKey(targets, component.OwnerTargetKey)
			ownerRowKey := cleanupPlanOwnerRowKey(rows, ownerTarget)
			switch {
			case rows[i].Key == ownerRowKey:
				rows[i].Relation = CleanupPlanRelationOwner
				rows[i].PhysicalBytes = component.Owner.Size
			case rows[i].CanonicalPath == component.CanonicalPath:
				rows[i].Relation = CleanupPlanRelationExact
			default:
				rows[i].Relation = CleanupPlanRelationNested
			}
			if component.Selection == CleanupPlanLocked &&
				rows[i].PolicySelection != CleanupPlanLocked {
				code := CleanupPlanReasonOverlapsLockedTarget
				description := "overlaps a hard-locked cleanup target"
				if cleanupPlanRowContainsLockedTarget(
					rows[i].CanonicalPath,
					targets,
					component.Key,
				) {
					code = CleanupPlanReasonContainsLockedTarget
					description = "contains a hard-locked cleanup target"
				}
				rows[i].Reasons = appendUniqueCleanupPlanReason(rows[i].Reasons, CleanupPlanReason{
					Code:        code,
					Description: description,
				})
			}
		}
		components = append(components, *component)
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].Key < components[j].Key
	})
	return components
}

func aggregateCleanupPlanComponentSelection(
	current CleanupPlanSelection,
	next CleanupPlanSelection,
) CleanupPlanSelection {
	if current == CleanupPlanLocked || next == CleanupPlanLocked {
		return CleanupPlanLocked
	}
	if current == CleanupPlanSelected || next == CleanupPlanSelected {
		return CleanupPlanSelected
	}
	return CleanupPlanUnselected
}

func cleanupPhysicalTargetByKey(
	targets []CleanupPhysicalTarget,
	key string,
) *CleanupPhysicalTarget {
	for i := range targets {
		if targets[i].Key == key {
			return &targets[i]
		}
	}
	return nil
}

func cleanupPlanOwnerRowKey(
	rows []CleanupPlanRow,
	target *CleanupPhysicalTarget,
) string {
	if target == nil {
		return ""
	}
	for _, row := range rows {
		if row.TargetKey == target.Key &&
			cleanTargetStableKey(row.Item) == cleanTargetStableKey(target.Item) {
			return row.Key
		}
	}
	if len(target.RowKeys) > 0 {
		return target.RowKeys[0]
	}
	return ""
}

func cleanupPlanRowContainsLockedTarget(
	rowPath string,
	targets []CleanupPhysicalTarget,
	ownerKey string,
) bool {
	for _, target := range targets {
		if target.OwnerKey == ownerKey &&
			target.PolicySelection == CleanupPlanLocked &&
			cleanTargetContains(rowPath, target.Key) {
			return true
		}
	}
	return false
}

func appendUniqueCleanupPlanReason(
	reasons []CleanupPlanReason,
	reason CleanupPlanReason,
) []CleanupPlanReason {
	for _, existing := range reasons {
		if existing.Code == reason.Code && existing.Description == reason.Description {
			return reasons
		}
	}
	return append(reasons, reason)
}

func cleanupPlanRowStableKey(row CleanupPlanRow) string {
	return strings.Join([]string{
		row.OwnerKey,
		row.CanonicalPath,
		row.Key,
		cleanTargetStableKey(row.Item),
	}, "\x00")
}

func sortedProviderErrors(providerErrors []types.ScanProviderError) []types.ScanProviderError {
	sorted := append([]types.ScanProviderError(nil), providerErrors...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Tool == sorted[j].Tool {
			return sorted[i].Message < sorted[j].Message
		}
		return sorted[i].Tool < sorted[j].Tool
	})
	return sorted
}
