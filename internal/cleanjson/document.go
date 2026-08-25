// Package cleanjson is the cobra-free clean-plan document: schema, evidence,
// policy block, totals, physical targets, rows, reason codes, and snapshot
// accounting. Tests should assert this model rather than cobra output snapshots.
//
// Wire fields must not expose canonical paths, hashed path IDs, or
// execution-layer receipt types. Paths stay redacted unless IncludePaths is
// set. Uniqueness stays reviewable, never recommended.
package cleanjson

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

const SchemaVersion = 1

const (
	DecisionSelected   = "selected"
	DecisionReviewable = "reviewable"
	DecisionProtected  = "protected"
	DecisionSkipped    = "skipped"

	PolicyEligible    = "eligible"
	PolicyRecommended = "recommended"
	PolicyReviewable  = "reviewable"
	PolicyProtected   = "protected"
	PolicySkipped     = "skipped"

	RelationOwner    = "owner"
	RelationExact    = "exact"
	RelationNested   = "nested"
	RelationAncestor = "ancestor"

	SourceLive = "live"
)

const (
	overlapOwner      = "physical-owner"
	overlapExact      = "exact-path"
	overlapDescendant = "nested-descendant"
	overlapAncestor   = "containing-ancestor"

	planSelected = "selected"
	planLocked   = "locked"

	documentTypePlan = "clean_plan"
	modeDryRun       = "dry_run"
)

// Plan is the machine-readable clean-plan document. It is the test surface
// for policy, reason-code, and snapshot-accounting mapping.
type Plan struct {
	SchemaVersion   int              `json:"schema_version"`
	DocumentType    string           `json:"document_type"`
	Mode            string           `json:"mode"`
	PathsIncluded   bool             `json:"paths_included"`
	Evidence        Evidence         `json:"evidence"`
	Policy          Policy           `json:"policy"`
	Totals          Totals           `json:"totals"`
	PhysicalTargets []PhysicalTarget `json:"physical_targets"`
	Rows            []Row            `json:"rows"`
}

type Evidence struct {
	Complete   bool   `json:"complete"`
	Source     string `json:"source"`
	ObservedAt string `json:"observed_at"`
}

type Policy struct {
	MinimumAge             string   `json:"minimum_age"`
	GuidedMinIdleAge       string   `json:"guided_min_idle_age,omitempty"`
	AgentStateGrace        string   `json:"agent_state_grace"`
	Categories             []string `json:"categories"`
	Tools                  []string `json:"tools"`
	Risky                  bool     `json:"risky"`
	IncludeActiveWorktrees bool     `json:"include_active_worktrees"`
}

type Totals struct {
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

type PhysicalTarget struct {
	ID          string  `json:"id"`
	Decision    string  `json:"decision"`
	Bytes       int64   `json:"bytes"`
	Category    string  `json:"category"`
	Tool        string  `json:"tool"`
	CleanupKind string  `json:"cleanup_kind"`
	Path        *string `json:"path,omitempty"`
}

type Row struct {
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

// SnapshotComponent is one containment-connected mutation owner after
// disjoint byte accounting. Receipt execution uses it; it is not a wire type.
type SnapshotComponent struct {
	Key             string
	Owner           types.DebrisInfo
	Decision        string
	AccountingBytes int64
	Rows            []SnapshotRow
}

type SnapshotRow struct {
	Item           types.DebrisInfo
	Relation       string
	PolicyDecision string
	Decision       string
	ReasonCodes    []string
	SortKey        string
}

// Source identifies the scan that produced the inventory.
type Source struct {
	Kind       string
	ObservedAt time.Time
}

// PlanEvidence is scan completeness and observation time used by the document.
type PlanEvidence struct {
	ObservedAt     time.Time
	ProviderErrors []types.ScanProviderError
}

// GuidedPolicy is the guided-review age that belongs in the policy block.
type GuidedPolicy struct {
	MinIdleAge time.Duration
}

// UnifiedPlan is the already-built planner result. Callers reuse
// BuildUnifiedCleanupPlan rather than re-extracting it here.
type UnifiedPlan struct {
	Components []PlanComponent
	Rows       []PlanRow
}

type PlanComponent struct {
	Key           string
	CanonicalPath string
	Owner         types.DebrisInfo
	Selection     string
}

type PlanRow struct {
	OwnerKey        string
	Item            types.DebrisInfo
	Relation        string
	PolicyDecision  string
	PolicySelection string
	Selection       string
	Reasons         []string
}

// AuditComponent is one overlap/audit physical unit. Callers adapt cmd overlap
// types rather than moving the overlap planner.
type AuditComponent struct {
	CanonicalPath string
	Owner         types.DebrisInfo
	Refusal       *cleaner.OverlapSafetyRefusal
	LogicalRows   []AuditRow
}

type AuditRow struct {
	Item           types.DebrisInfo
	CanonicalPath  string
	Relation       string
	PolicyDecision string
	ReasonCodes    []string
}

// Input is the cobra-free build boundary: a complete scan, an already-built
// unified plan, and the audit/protection snapshot used to project the document.
type Input struct {
	Result       *types.ScanResult
	Source       Source
	Opts         types.PruneOptions
	Guided       *GuidedPolicy
	IncludePaths bool
	Plan         UnifiedPlan
	Evidence     PlanEvidence
	Audit        []AuditComponent
	Inventory    []types.DebrisInfo
	Protections  map[string]string
}

// Build projects an already-normalized unified plan and audit snapshot into
// the public plan document. It refuses a nil or partial scan before emission.
func Build(in Input) (Plan, error) {
	if in.Result == nil {
		return Plan{}, fmt.Errorf("nil cleanup scan result")
	}
	if err := refusePartialScan(in.Result); err != nil {
		return Plan{}, err
	}
	inventory := in.Inventory
	if inventory == nil {
		inventory = in.Result.Worktrees
	}
	components := SnapshotComponents(in.Plan, in.Audit, inventory, in.Protections)
	return Render(in, components), nil
}

// Render projects already-normalized snapshot components into the public plan
// document. Callers that already hold the accepted plan render from it
// directly instead of rebuilding one from default candidates.
func Render(in Input, components []SnapshotComponent) Plan {
	return Plan{
		SchemaVersion:   SchemaVersion,
		DocumentType:    documentTypePlan,
		Mode:            modeDryRun,
		PathsIncluded:   in.IncludePaths,
		Evidence:        evidenceFor(in.Source, in.Evidence),
		Policy:          PolicyFor(in.Opts, in.Guided),
		Totals:          totalsFor(components),
		PhysicalTargets: physicalTargetsFor(components, in.IncludePaths),
		Rows:            rowsFor(components, in.IncludePaths),
	}
}

// Encode writes one indented plan document.
func Encode(output io.Writer, document Plan) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

func refusePartialScan(result *types.ScanResult) error {
	if result == nil || !result.Partial() {
		return nil
	}
	providers := make([]string, 0, len(result.ProviderErrors))
	for _, providerErr := range result.ProviderErrors {
		providers = append(providers, string(providerErr.Tool))
	}
	return fmt.Errorf("cleanup requires a complete scan; failed providers: %s", strings.Join(providers, ", "))
}

func evidenceFor(source Source, evidence PlanEvidence) Evidence {
	sourceName := source.Kind
	if sourceName == "" {
		sourceName = SourceLive
	}
	observedAt := evidence.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	return Evidence{
		Complete:   len(evidence.ProviderErrors) == 0,
		Source:     sourceName,
		ObservedAt: observedAt.Format(time.RFC3339Nano),
	}
}

func cleanupKind(item types.DebrisInfo) types.CleanupKind {
	if item.CleanupKind != "" {
		return item.CleanupKind
	}
	return types.CleanupRemovePath
}
