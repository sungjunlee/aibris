package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

type cleanupOverlapSafetyRuntime struct {
	Initial cleaner.OverlapSafetyEvidence
	Refresh func(context.Context) (cleaner.OverlapSafetyEvidence, error)
	Lookup  cleaner.AgentStateRevalidatorLookup
	// memo, when non-nil, collapses the expensive full agent-state re-scan
	// (Refresh) so per-target mutation barriers within one execution batch
	// share a single scan instead of re-running it per target. The cached scan
	// is only reused while fingerprint reports an unchanged agent-state entry
	// set; any added, removed, or renamed entry invalidates it and forces a
	// fresh full scan so newly created overlapping state is still discovered
	// before the next mutation. Classification drift of entries already known
	// to overlap a target is independently caught by the per-obligation
	// RevalidateAgentState inside ValidateBeforeMutationWithReport, which runs
	// live for every target. A nil memo disables memoization (tests build
	// runtimes this way and keep the previous per-item refresh behavior).
	memo *refreshMemo
	// fingerprint cheaply enumerates the current agent-state entry set (entry
	// directory names under the agent-state store roots, no jsonl parsing or
	// size walking). It enumerates the roots from adapter.AgentStateStoreRoots(),
	// the single source of truth shared with the agent-state providers, so a
	// newly added agent-state root automatically flows to the fingerprint.
	fingerprint func(context.Context) (string, error)
}

// refreshMemo caches one full agent-state re-scan for the lifetime of a single
// execution batch, keyed by a cheap entry-set fingerprint so that additions,
// removals, or renames of agent-state entries invalidate the cache and force a
// fresh scan. It is safe for concurrent use; executePreparedCleanTargets runs
// targets sequentially today, but the barrier contract does not require it.
type refreshMemo struct {
	mu       sync.Mutex
	loaded   bool
	key      string
	evidence cleaner.OverlapSafetyEvidence
}

// get returns the cached evidence when the fingerprint key is unchanged,
// otherwise runs refresh and caches a successful result. Errors are never
// cached: a failed scan leaves the previous state untouched so the next target
// retries instead of inheriting one target's transient failure. A fingerprint
// error fails closed by forcing a rescan.
func (m *refreshMemo) get(
	ctx context.Context,
	fingerprint func(context.Context) (string, error),
	refresh func(context.Context) (cleaner.OverlapSafetyEvidence, error),
) (cleaner.OverlapSafetyEvidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ""
	if fingerprint != nil {
		if fp, err := fingerprint(ctx); err == nil {
			key = fp
		}
		// On fingerprint error key stays "" so the cache is never reused
		// (fail closed: an unverifiable entry set must trigger a fresh scan).
	}
	if m.loaded && fingerprint != nil && key != "" && key == m.key {
		return m.evidence, nil
	}
	evidence, err := refresh(ctx)
	if err != nil {
		return evidence, err
	}
	m.evidence = evidence
	m.key = key
	m.loaded = true
	return evidence, nil
}

func (m *refreshMemo) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaded = false
	m.key = ""
	m.evidence = cleaner.OverlapSafetyEvidence{}
}

// refreshedEvidence returns the refreshed overlap evidence, memoizing the full
// re-scan across a batch when a memo is configured and the entry-set
// fingerprint is unchanged.
func (r cleanupOverlapSafetyRuntime) refreshedEvidence(
	ctx context.Context,
) (cleaner.OverlapSafetyEvidence, error) {
	if r.memo != nil {
		return r.memo.get(ctx, r.fingerprint, r.Refresh)
	}
	return r.Refresh(ctx)
}

// resetRefreshMemo clears any cached re-scan so the next batch starts fresh.
func (r cleanupOverlapSafetyRuntime) resetRefreshMemo() {
	if r.memo != nil {
		r.memo.reset()
	}
}

// agentStateEntryFingerprint enumerates the current agent-state entry set by
// listing immediate child directory names under each agent-state store root.
// This mirrors how the Claude/Cursor providers enumerate entries (each child
// directory is one entry) without parsing jsonl or walking sizes, so it is
// cheap enough to run before every mutation. Adding, removing, or renaming an
// entry changes the returned key. Names are joined with \x00, which cannot
// appear in a filename, so the key is injective in the entry set (a directory
// named "a,b" can never alias two directories "a" and "b").
func agentStateEntryFingerprint(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	roots, err := adapter.AgentStateStoreRoots()
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(roots))
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				parts = append(parts, root+"\x00<absent>")
				continue
			}
			return "", err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		parts = append(parts, root+"\x00"+strings.Join(names, "\x00"))
	}
	return strings.Join(parts, "|"), nil
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
		Initial:     initial,
		Refresh:     scanEvidence,
		Lookup:      lookup,
		memo:        &refreshMemo{},
		fingerprint: agentStateEntryFingerprint,
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
	targets = normalizeCleanTargets(targets)
	sort.Slice(targets, func(i, j int) bool {
		left, _ := cleanTargetPathKey(targets[i].Path)
		right, _ := cleanTargetPathKey(targets[j].Path)
		if left == right {
			return cleanTargetStableKey(targets[i]) < cleanTargetStableKey(targets[j])
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
			path, ok := cleanTargetPathKey(input.Item.Path)
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
			return cleanTargetStableKey(components[i].Owner) < cleanTargetStableKey(components[j].Owner)
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
	case cleanTargetContains(ownerPath, rowPath):
		return cleanupOverlapDescendant, true
	case cleanTargetContains(rowPath, ownerPath):
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
		matchPath, ok := cleanTargetPathKey(match.Item.Path)
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
				refusalPath, refusalOK := cleanTargetPathKey(component.Refusal.AgentStatePath)
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
		matchPath, ok := cleanTargetPathKey(match.Item.Path)
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
			cleanTargetStableKey(row.Item) == cleanTargetStableKey(owner) {
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
			cleanTargetStableKey(rows[i].Item) == cleanTargetStableKey(owner) {
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
		cleanTargetStableKey(row.Item) == cleanTargetStableKey(owner) {
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
	refreshed, err := s.runtime.refreshedEvidence(ctx)
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
