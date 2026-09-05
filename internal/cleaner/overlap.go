package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

type agentStateSafetyEntry struct {
	item     types.DebrisInfo
	identity canonicalPathIdentity
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

func canonicalAgentStateEntries(ctx context.Context, items []types.DebrisInfo) ([]agentStateSafetyEntry, []types.DebrisInfo) {
	var entries []agentStateSafetyEntry
	var ambiguous []types.DebrisInfo
	for _, item := range items {
		if item.Category != types.CategoryAgentState {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		identity, err := canonicalExistingPathIdentity(item.Path)
		if err != nil {
			ambiguous = append(ambiguous, item)
			continue
		}
		entries = append(entries, agentStateSafetyEntry{item: item, identity: identity})
	}
	sort.Slice(entries, func(i, j int) bool {
		return agentStateEntryStableKey(entries[i]) < agentStateEntryStableKey(entries[j])
	})
	sort.Slice(ambiguous, func(i, j int) bool {
		return agentStateItemStableKey(ambiguous[i]) < agentStateItemStableKey(ambiguous[j])
	})
	return entries, ambiguous
}

func buildOverlapSafetyComponent(
	target types.DebrisInfo,
	entries []agentStateSafetyEntry,
	ambiguousEntries []types.DebrisInfo,
	lookup AgentStateRevalidatorLookup,
) OverlapSafetyComponent {
	component := OverlapSafetyComponent{Target: target}
	targetIdentity, err := canonicalExistingPathIdentity(target.Path)
	if err != nil {
		component.Refusal = overlapRefusal(
			OverlapSafetyAmbiguousIdentity, target.Path, "", "", err.Error())
		return component
	}
	component.targetIdentity = targetIdentity
	component.CanonicalPath = targetIdentity.canonical

	for _, item := range ambiguousEntries {
		mayOverlap, resolutionErr := ambiguousAgentStateMayOverlap(targetIdentity, item.Path)
		if !mayOverlap && resolutionErr == nil {
			continue
		}
		component.Matches = append(component.Matches, OverlapSafetyMatch{
			Item:     item,
			Relation: OverlapRelationAmbiguous,
		})
		detail := "agent-state path cannot be canonically resolved"
		if resolutionErr != nil {
			detail += ": " + resolutionErr.Error()
		}
		component.Refusal = overlapRefusal(
			OverlapSafetyAmbiguousIdentity, target.Path, item.Tool, item.Path,
			detail)
		return component
	}

	for _, entry := range entries {
		relation, overlaps := canonicalOverlapRelation(targetIdentity.canonical, entry.identity.canonical)
		if !overlaps {
			continue
		}
		component.Matches = append(component.Matches, OverlapSafetyMatch{
			Item:     entry.item,
			Relation: relation,
		})
	}

	if cleanupKind(target) == types.CleanupCommand && len(component.Matches) > 0 {
		component.Refusal = overlapRefusal(
			OverlapSafetyCommandOverlap, target.Path, component.Matches[0].Item.Tool,
			component.Matches[0].Item.Path,
			"declared command path does not prove subtree-removal semantics")
		return component
	}

	for _, match := range component.Matches {
		if match.Item.Classification == types.EntryClassOrphaned {
			continue
		}
		component.Refusal = overlapRefusal(
			protectedOverlapReason(match.Relation), target.Path, match.Item.Tool,
			match.Item.Path,
			fmt.Sprintf("classified %s", protectedEntryClass(match.Item.Classification)))
		return component
	}

	obligations := make(map[string]AgentStateObligation)
	for _, match := range component.Matches {
		entry := entryForMatch(entries, match)
		if entry == nil {
			continue
		}
		registration, registrationErr := lookupAgentStateRevalidator(lookup, match.Item.Tool)
		if registrationErr != nil {
			component.Refusal = overlapRefusal(
				OverlapSafetyNestedRevalidation, target.Path, match.Item.Tool,
				match.Item.Path,
				registrationErr.Error())
			return component
		}
		obligation := AgentStateObligation{
			Tool:         match.Item.Tool,
			EntryPath:    entry.identity.canonical,
			ProviderID:   registration.ProviderID,
			pathIdentity: entry.identity,
		}
		obligations[agentStateObligationKey(obligation)] = obligation
	}
	for _, obligation := range obligations {
		component.Obligations = append(component.Obligations, obligation)
	}
	sort.Slice(component.Obligations, func(i, j int) bool {
		return agentStateObligationKey(component.Obligations[i]) <
			agentStateObligationKey(component.Obligations[j])
	})
	return component
}

func lookupAgentStateRevalidator(
	lookup AgentStateRevalidatorLookup,
	tool types.Tool,
) (adapter.AgentStateRevalidatorRegistration, error) {
	if lookup == nil {
		return adapter.AgentStateRevalidatorRegistration{},
			fmt.Errorf("%w for tool %q", adapter.ErrAgentStateRevalidatorMissing, tool)
	}
	registration, err := lookup(tool)
	if err != nil {
		return adapter.AgentStateRevalidatorRegistration{}, err
	}
	if registration.Revalidator == nil || registration.ProviderID == "" {
		return adapter.AgentStateRevalidatorRegistration{},
			fmt.Errorf("%w for tool %q", adapter.ErrAgentStateRevalidatorMissing, tool)
	}
	return registration, nil
}

func entryForMatch(entries []agentStateSafetyEntry, match OverlapSafetyMatch) *agentStateSafetyEntry {
	for i := range entries {
		if entries[i].item.Path == match.Item.Path &&
			entries[i].item.Tool == match.Item.Tool &&
			entries[i].item.ID == match.Item.ID {
			return &entries[i]
		}
	}
	return nil
}

func overlapRefusal(
	reason OverlapSafetyReason,
	targetPath string,
	agentStateTool types.Tool,
	agentStatePath string,
	detail string,
) *OverlapSafetyRefusal {
	return &OverlapSafetyRefusal{
		Reason:         reason,
		TargetPath:     targetPath,
		AgentStateTool: agentStateTool,
		AgentStatePath: agentStatePath,
		Detail:         detail,
	}
}

func protectedOverlapReason(relation OverlapSafetyRelation) OverlapSafetyReason {
	switch relation {
	case OverlapRelationAgentStateAncestor:
		return OverlapSafetyProtectedAncestor
	case OverlapRelationExact:
		return OverlapSafetyProtectedExact
	default:
		return OverlapSafetyProtectedDescendant
	}
}

func protectedEntryClass(classification types.EntryClass) types.EntryClass {
	if classification == types.EntryClassLive {
		return classification
	}
	return types.EntryClassUndetermined
}

func agentStateObligationKey(obligation AgentStateObligation) string {
	return string(obligation.Tool) + "\x00" + obligation.EntryPath
}

func agentStateEntryStableKey(entry agentStateSafetyEntry) string {
	return entry.identity.canonical + "\x00" + agentStateItemStableKey(entry.item)
}

func agentStateItemStableKey(item types.DebrisInfo) string {
	return strings.Join([]string{
		string(item.Tool),
		item.ID,
		item.Path,
		string(item.Classification),
	}, "\x00")
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
