package cleaner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	ErrIncompleteOverlapSafetyEvidence = errors.New("overlap safety evidence is incomplete")
	ErrOverlapSafetyRefusal            = errors.New("overlap safety refusal")
)

// OverlapSafetyEvidence deliberately keeps the complete scan inventory
// separate from ordinary eligible candidates. Complete must only be set by a
// caller that attempted every registered provider in its scan scope.
type OverlapSafetyEvidence struct {
	Items          []types.DebrisInfo
	ProviderErrors []types.ScanProviderError
	Complete       bool
}

type OverlapSafetyReason string

const (
	OverlapSafetyProtectedAncestor   OverlapSafetyReason = "protected agent-state ancestor"
	OverlapSafetyProtectedDescendant OverlapSafetyReason = "protected agent-state descendant"
	OverlapSafetyProtectedExact      OverlapSafetyReason = "protected agent-state exact overlap"
	OverlapSafetyAmbiguousIdentity   OverlapSafetyReason = "ambiguous overlap path identity"
	OverlapSafetyCommandOverlap      OverlapSafetyReason = "cleanup command overlaps agent-state"
	OverlapSafetyNestedRevalidation  OverlapSafetyReason = "nested agent-state revalidation refused"
)

type OverlapSafetyRelation string

const (
	OverlapRelationAgentStateAncestor   OverlapSafetyRelation = "agent-state-ancestor"
	OverlapRelationAgentStateDescendant OverlapSafetyRelation = "agent-state-descendant"
	OverlapRelationExact                OverlapSafetyRelation = "exact"
	OverlapRelationAmbiguous            OverlapSafetyRelation = "ambiguous"
)

type AgentStateRevalidatorLookup func(types.Tool) (adapter.AgentStateRevalidatorRegistration, error)

type AgentStateObligation struct {
	Tool         types.Tool
	EntryPath    string
	ProviderID   string
	pathIdentity canonicalPathIdentity
}

type AgentStateRevalidationState string

const (
	AgentStateRevalidationPassed       AgentStateRevalidationState = "passed"
	AgentStateRevalidationBlocked      AgentStateRevalidationState = "blocked"
	AgentStateRevalidationNotAttempted AgentStateRevalidationState = "not-attempted"
)

// AgentStateRevalidationOutcome records the result of one canonical nested
// obligation. It is an internal execution receipt, not a public wire schema.
type AgentStateRevalidationOutcome struct {
	Tool           types.Tool
	EntryPath      string
	ProviderID     string
	State          AgentStateRevalidationState
	Classification types.EntryClass
	Reason         string
}

// OverlapSafetyValidation records component-level and per-obligation lineage
// from the final pre-mutation barrier.
type OverlapSafetyValidation struct {
	Obligations    []AgentStateRevalidationOutcome
	BlockingPath   string
	BlockingReason string
}

type OverlapSafetyMatch struct {
	Item     types.DebrisInfo
	Relation OverlapSafetyRelation
}

type OverlapSafetyRefusal struct {
	Reason         OverlapSafetyReason
	TargetPath     string
	AgentStateTool types.Tool
	AgentStatePath string
	Detail         string
}

func (r *OverlapSafetyRefusal) Error() string {
	if r == nil {
		return ""
	}
	message := fmt.Sprintf("%s for %q", r.Reason, r.TargetPath)
	if r.AgentStatePath != "" {
		message += fmt.Sprintf(" at %q", r.AgentStatePath)
	}
	if r.Detail != "" {
		message += ": " + r.Detail
	}
	return message
}

type OverlapSafetyComponent struct {
	Target        types.DebrisInfo
	CanonicalPath string
	Matches       []OverlapSafetyMatch
	Obligations   []AgentStateObligation
	Refusal       *OverlapSafetyRefusal

	targetIdentity canonicalPathIdentity
}

type OverlapSafetyPlan struct {
	Components []OverlapSafetyComponent
}

func (p OverlapSafetyPlan) AllowedTargets() []types.DebrisInfo {
	targets := make([]types.DebrisInfo, 0, len(p.Components))
	for _, component := range p.Components {
		if component.Refusal == nil {
			targets = append(targets, component.Target)
		}
	}
	return targets
}

func (p OverlapSafetyPlan) ComponentForTarget(target types.DebrisInfo) (OverlapSafetyComponent, bool) {
	for _, component := range p.Components {
		if component.Target.Path == target.Path &&
			component.Target.Category == target.Category &&
			component.Target.Tool == target.Tool &&
			component.Target.ID == target.ID {
			return component, true
		}
	}
	return OverlapSafetyComponent{}, false
}

// BuildOverlapSafetyPlan attaches every scanned agent-state lock and
// revalidation obligation to the selected physical candidates. It refuses to
// operate on partial evidence even when called outside the CLI scan gate.
func BuildOverlapSafetyPlan(
	ctx context.Context,
	evidence OverlapSafetyEvidence,
	candidates []types.DebrisInfo,
	lookup AgentStateRevalidatorLookup,
) (OverlapSafetyPlan, error) {
	if err := ctx.Err(); err != nil {
		return OverlapSafetyPlan{}, err
	}
	if !evidence.Complete || len(evidence.ProviderErrors) > 0 {
		return OverlapSafetyPlan{}, fmt.Errorf("%w: complete=%t, provider errors=%d",
			ErrIncompleteOverlapSafetyEvidence, evidence.Complete, len(evidence.ProviderErrors))
	}

	entries, ambiguousEntries := canonicalAgentStateEntries(ctx, evidence.Items)
	plan := OverlapSafetyPlan{Components: make([]OverlapSafetyComponent, 0, len(candidates))}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return OverlapSafetyPlan{}, err
		}
		component := buildOverlapSafetyComponent(candidate, entries, ambiguousEntries, lookup)
		plan.Components = append(plan.Components, component)
	}
	return plan, nil
}

func PathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
