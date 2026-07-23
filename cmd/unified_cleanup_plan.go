package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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

type CleanupPlanReasonCode string

const (
	CleanupPlanReasonClassicEligible        CleanupPlanReasonCode = "classic_eligible"
	CleanupPlanReasonContainsLockedTarget   CleanupPlanReasonCode = "contains_locked_target"
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
	RowKey    string
	Item      types.DebrisInfo
	Selection CleanupPlanSelection
	Reasons   []CleanupPlanReason
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
	Key       string
	TargetKey string
	Item      types.DebrisInfo
	Selection CleanupPlanSelection
	Reasons   []CleanupPlanReason
}

// CleanupPhysicalTarget is one exact canonical path. Nested physical targets
// remain distinct here; selection accounting normalizes their path coverage.
type CleanupPhysicalTarget struct {
	Key       string
	Item      types.DebrisInfo
	RowKeys   []string
	Selection CleanupPlanSelection
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
	Rows     []CleanupPlanRow
	Targets  []CleanupPhysicalTarget
	Evidence CleanupPlanEvidence
}

type cleanupPlanTargetGroup struct {
	key        string
	candidates []CleanupPlanCandidate
}

// BuildUnifiedCleanupPlan normalizes visible policy decisions into exact
// physical targets. A hard lock on a target or any descendant dominates
// selections that could remove it.
func BuildUnifiedCleanupPlan(ctx context.Context, candidates []CleanupPlanCandidate, evidence CleanupPlanEvidence) (UnifiedCleanupPlan, error) {
	if err := ctx.Err(); err != nil {
		return UnifiedCleanupPlan{}, err
	}

	ordered := append([]CleanupPlanCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return cleanupPlanCandidateStableKey(ordered[i]) < cleanupPlanCandidateStableKey(ordered[j])
	})

	rowKeys := make(map[string]bool, len(ordered))
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
		candidate.Item.Path = path
		if candidate.RowKey == "" {
			candidate.RowKey = cleanupPlanCandidateStableKey(candidate)
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
		item := cleanupPlanRepresentative(group.candidates)
		target := CleanupPhysicalTarget{Key: key, Item: item, Selection: selection}
		for _, candidate := range group.candidates {
			target.RowKeys = append(target.RowKeys, candidate.RowKey)
			rows = append(rows, CleanupPlanRow{
				Key:       candidate.RowKey,
				TargetKey: key,
				Item:      candidate.Item,
				Selection: selection,
				Reasons:   append([]CleanupPlanReason(nil), candidate.Reasons...),
			})
		}
		targets = append(targets, target)
	}

	propagateCleanupPlanLocks(rows, targets)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Key < rows[j].Key
	})
	evidence.ProviderErrors = sortedProviderErrors(evidence.ProviderErrors)
	return UnifiedCleanupPlan{Rows: rows, Targets: targets, Evidence: evidence}, nil
}

// ClassicCleanupPlanCandidates adapts already-filtered classic targets.
func ClassicCleanupPlanCandidates(targets []types.DebrisInfo) []CleanupPlanCandidate {
	candidates := make([]CleanupPlanCandidate, 0, len(targets))
	for _, target := range targets {
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:    "classic:" + cleanTargetStableKey(target),
			Item:      target,
			Selection: CleanupPlanSelected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonClassicEligible,
				Description: "eligible under classic cleanup filters",
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
			RowKey:    "worktree:" + cleanupUnitStableKey(decision.Unit),
			Item:      guidedCleanupUnitItem(decision.Unit, items),
			Selection: cleanupPlanSelectionForDecision(decision.Class),
			Reasons:   reasons,
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
	selected := make([]types.DebrisInfo, 0, len(p.Targets))
	for _, target := range p.Targets {
		if target.Selection == CleanupPlanSelected {
			selected = append(selected, target.Item)
		}
	}
	return normalizeCleanTargets(selected)
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
	physical := normalizeCleanupPlanPhysicalTargets(p.Targets)
	totals.PhysicalTargets = len(physical)
	for _, target := range physical {
		totals.PhysicalBytes += target.Size
	}
	selected := p.SelectedPhysicalTargets()
	totals.SelectedTargets = len(selected)
	for _, target := range selected {
		totals.SelectedBytes += target.Size
	}
	eligible := normalizedCleanupPlanTargetsBySelection(p.Targets, CleanupPlanSelected, CleanupPlanUnselected)
	totals.EligibleTargets = len(eligible)
	for _, target := range eligible {
		totals.EligibleBytes += target.Size
	}
	reviewable := normalizedCleanupPlanTargetsBySelection(p.Targets, CleanupPlanUnselected)
	totals.ReviewableTargets = len(reviewable)
	for _, target := range reviewable {
		totals.ReviewableBytes += target.Size
	}
	locked := normalizedCleanupPlanTargetsBySelection(p.Targets, CleanupPlanLocked)
	totals.HardLockedTargets = len(locked)
	for _, target := range locked {
		totals.HardLockedBytes += target.Size
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

func cleanupPlanRepresentative(candidates []CleanupPlanCandidate) types.DebrisInfo {
	item := candidates[0].Item
	maxSize := item.Size
	for _, candidate := range candidates[1:] {
		if preferCleanTarget(candidate.Item, item) {
			item = candidate.Item
		}
		if candidate.Item.Size > maxSize {
			maxSize = candidate.Item.Size
		}
	}
	item.Size = maxSize
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

func propagateCleanupPlanLocks(rows []CleanupPlanRow, targets []CleanupPhysicalTarget) {
	lockedPaths := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Selection == CleanupPlanLocked {
			lockedPaths = append(lockedPaths, target.Key)
		}
	}
	for i := range targets {
		if targets[i].Selection == CleanupPlanLocked {
			continue
		}
		for _, lockedPath := range lockedPaths {
			if cleanTargetContains(targets[i].Key, lockedPath) {
				targets[i].Selection = CleanupPlanLocked
				for j := range rows {
					if rows[j].TargetKey != targets[i].Key {
						continue
					}
					rows[j].Selection = CleanupPlanLocked
					rows[j].Reasons = append(rows[j].Reasons, CleanupPlanReason{
						Code:        CleanupPlanReasonContainsLockedTarget,
						Description: "contains a hard-locked cleanup target",
					})
				}
				break
			}
		}
	}
}

func normalizeCleanupPlanPhysicalTargets(targets []CleanupPhysicalTarget) []types.DebrisInfo {
	items := make([]types.DebrisInfo, 0, len(targets))
	for _, target := range targets {
		items = append(items, target.Item)
	}
	return normalizeCleanTargets(items)
}

func normalizedCleanupPlanTargetsBySelection(targets []CleanupPhysicalTarget, selections ...CleanupPlanSelection) []types.DebrisInfo {
	allowed := make(map[CleanupPlanSelection]bool, len(selections))
	for _, selection := range selections {
		allowed[selection] = true
	}
	items := make([]types.DebrisInfo, 0, len(targets))
	for _, target := range targets {
		if allowed[target.Selection] {
			items = append(items, target.Item)
		}
	}
	return normalizeCleanTargets(items)
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
