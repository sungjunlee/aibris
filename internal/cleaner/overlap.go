package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
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

type canonicalPathIdentity struct {
	raw       string
	canonical string
	info      os.FileInfo
}

func canonicalExistingPathIdentity(path string) (canonicalPathIdentity, error) {
	raw := strings.TrimSpace(path)
	if raw == "" || !filepath.IsAbs(raw) {
		return canonicalPathIdentity{}, fmt.Errorf("path is not absolute")
	}
	raw = filepath.Clean(raw)
	canonical, err := filepath.EvalSymlinks(raw)
	if err != nil {
		return canonicalPathIdentity{}, fmt.Errorf("resolving %q: %w", raw, err)
	}
	canonical = filepath.Clean(canonical)
	info, err := os.Stat(canonical)
	if err != nil {
		return canonicalPathIdentity{}, fmt.Errorf("reading %q: %w", canonical, err)
	}
	return canonicalPathIdentity{raw: raw, canonical: canonical, info: info}, nil
}

func (identity canonicalPathIdentity) unchanged() error {
	current, err := canonicalExistingPathIdentity(identity.raw)
	if err != nil {
		return err
	}
	return identity.matches(current)
}

func (identity canonicalPathIdentity) matches(current canonicalPathIdentity) error {
	if identity.canonical != current.canonical {
		return fmt.Errorf("canonical path changed from %q to %q", identity.canonical, current.canonical)
	}
	if identity.info == nil || current.info == nil || !os.SameFile(identity.info, current.info) {
		return fmt.Errorf("filesystem identity changed at %q", identity.canonical)
	}
	return nil
}

func canonicalOverlapRelation(target, agentState string) (OverlapSafetyRelation, bool) {
	if target == agentState {
		return OverlapRelationExact, true
	}
	if PathContains(agentState, target) {
		return OverlapRelationAgentStateAncestor, true
	}
	if PathContains(target, agentState) {
		return OverlapRelationAgentStateDescendant, true
	}
	return "", false
}

func PathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func ambiguousAgentStateMayOverlap(
	target canonicalPathIdentity,
	agentStatePath string,
) (bool, error) {
	rawAgentState := filepath.Clean(strings.TrimSpace(agentStatePath))
	if !filepath.IsAbs(rawAgentState) {
		return false, nil
	}

	for _, targetPath := range []string{target.raw, target.canonical} {
		if _, overlaps := canonicalOverlapRelation(targetPath, rawAgentState); overlaps {
			return true, nil
		}
	}

	resolved, err := resolvePathWithUnresolvedSuffix(rawAgentState, 0)
	if err != nil {
		return false, fmt.Errorf("resolving %q: %w", rawAgentState, err)
	}
	if resolved == rawAgentState {
		return false, nil
	}
	for _, targetPath := range []string{target.raw, target.canonical} {
		if _, overlaps := canonicalOverlapRelation(targetPath, resolved); overlaps {
			return true, nil
		}
	}
	return false, nil
}

func resolvePathWithUnresolvedSuffix(path string, symlinkDepth int) (string, error) {
	if symlinkDepth > 255 {
		return "", fmt.Errorf("too many symlinks resolving %q", path)
	}

	current := filepath.Clean(path)
	var suffix []string
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(current)
				if err != nil {
					return "", err
				}
				if !filepath.IsAbs(linkTarget) {
					linkTarget = filepath.Join(filepath.Dir(current), linkTarget)
				}
				current, err = resolvePathWithUnresolvedSuffix(linkTarget, symlinkDepth+1)
				if err != nil {
					return "", err
				}
			} else {
				current, err = filepath.EvalSymlinks(current)
				if err != nil {
					return "", err
				}
			}
			for _, part := range suffix {
				current = filepath.Join(current, part)
			}
			return filepath.Clean(current), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}
