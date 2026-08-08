package types

import (
	"context"
	"time"
)

// Tool identifies the AI tool that created the debris.
type Tool string

const (
	ToolCodex       Tool = "codex"
	ToolClaude      Tool = "claude"
	ToolCursor      Tool = "cursor"
	ToolWindsurf    Tool = "windsurf"
	ToolNodeModules Tool = "node_modules"
	ToolUnknown     Tool = "unknown"
	ToolBuildCache  Tool = "build-cache"
	ToolPipCache    Tool = "pip-cache"
	ToolAILogs      Tool = "ai-logs"
)

// CleanupKind describes how an item should be cleaned.
type CleanupKind string

const (
	CleanupRemovePath CleanupKind = "remove-path"
	CleanupCommand    CleanupKind = "command"
)

// Category classifies the type of debris.
type Category string

const (
	CategoryWorktree    Category = "worktree"
	CategoryNodeModules Category = "node_modules"
	CategoryBuildCache  Category = "build-cache"
	CategoryOtherCache  Category = "other-cache"
	CategoryAgentState  Category = "agent-state"
	CategoryAILogs      Category = "ai-logs"
)

// IsRisky reports whether this category requires explicit --risky opt-in.
// AI logs and unknown categories default to risky (safe-by-default).
func (c Category) IsRisky() bool {
	switch c {
	case CategoryWorktree, CategoryNodeModules, CategoryBuildCache, CategoryOtherCache, CategoryAgentState:
		return false
	case "": // backward compat: pre-Category entries are safe
		return false
	default:
		return true
	}
}

// WorktreeStatus describes the health of a git worktree.
type WorktreeStatus string

const (
	WorktreeOrphaned WorktreeStatus = "orphaned"  // .git file exists but parent repo is gone
	WorktreeActive   WorktreeStatus = "active"    // .git file exists and parent repo is alive
	WorktreePlain    WorktreeStatus = "plain-dir" // no .git file (plain directory)
)

// EntryClass describes whether an agent-state entry's recorded working
// directory still exists.
type EntryClass string

const (
	EntryClassLive         EntryClass = "live"
	EntryClassOrphaned     EntryClass = "orphaned"
	EntryClassUndetermined EntryClass = "undetermined"
)

// DebrisInfo describes a single debris item found during scanning.
type DebrisInfo struct {
	Tool           Tool
	Category       Category
	ID             string
	Project        string
	Source         string
	Path           string
	Size           int64
	ModTime        time.Time
	Status         WorktreeStatus // empty for non-worktree debris
	Classification EntryClass     // empty for debris without entry classification
	Reason         string
	CleanupKind    CleanupKind
	CleanupCommand []string
	// ScanPathIdentity, ScanPathType, and ScanPathEvidenceRequired are transient
	// cleanup safety evidence.
	// They are deliberately excluded from persisted ScanResult JSON.
	ScanPathIdentity         string `json:"-"`
	ScanPathType             uint32 `json:"-"`
	ScanPathEvidenceRequired bool   `json:"-"`
}

// ScanResult aggregates all debris found by all adapters.
type ScanResult struct {
	Worktrees      []DebrisInfo
	TotalCount     int
	TotalSize      int64
	ByCategory     map[Category]CategorySummary
	ByTool         map[Tool]ToolSummary
	ProviderErrors []ScanProviderError
	// ExcludedByUser counts discovered items removed from results by user
	// exclusions. Exclusions affect discovery only and never broaden deletion
	// authority.
	ExcludedByUser   int               `json:"excluded_by_user,omitempty"`
	ExcludedScopes   []ExcludedScope   `json:"excluded_scopes,omitempty"`
	RejectedExcludes []RejectedExclude `json:"rejected_excludes,omitempty"`
	// Retention is a read-only protected-content inventory. It is never a
	// cleanup candidate, is absent from totals, and carries no member paths.
	Retention RetentionProjection
}

// Partial reports whether one or more providers failed while other usable
// scan results were retained.
func (r *ScanResult) Partial() bool {
	return r != nil && len(r.ProviderErrors) > 0
}

// ScanProviderError records a provider failure without discarding successful
// results from unrelated providers.
type ScanProviderError struct {
	Tool    Tool
	Message string
}

// RetentionStoreID identifies one exact protected-content inventory root.
// Retention store IDs are intentionally separate from cleanup tools and
// categories.
type RetentionStoreID string

// RetentionStoreCodexSessions inventories regular rollout files under the
// exact ~/.codex/sessions root as UTC-month aggregates.
const RetentionStoreCodexSessions RetentionStoreID = "codex-sessions"

// RetentionBucket is a read-only aggregate over protected content. It is
// never a cleanup candidate and contains no member paths or private metadata.
type RetentionBucket struct {
	StoreID       RetentionStoreID `json:"store_id"`
	BucketID      string           `json:"bucket_id"`
	UnitCount     int              `json:"unit_count"`
	MemberCount   int              `json:"member_count"`
	ApparentBytes int64            `json:"apparent_bytes"`
	OrphanedCount int              `json:"orphaned_count"`
	OrphanedBytes int64            `json:"orphaned_bytes"`
}

// RetentionProjection is a read-only view that remains outside ordinary scan
// totals and every cleanup authorization path.
type RetentionProjection struct {
	Buckets        []RetentionBucket        `json:"buckets"`
	Partial        bool                     `json:"partial"`
	ProviderErrors []RetentionProviderError `json:"provider_errors"`
}

// RetentionProvider inventories one exact protected-content store. Expected
// store-local failures are represented in the returned projection; context
// cancellation is returned as a hard error.
type RetentionProvider interface {
	Name() RetentionStoreID
	Scan(ctx context.Context, opts ScanOptions) (RetentionProjection, error)
}

// RetentionProviderError records a path-free provider diagnostic. It is
// separate from ordinary provider errors so it cannot disable ordinary clean.
type RetentionProviderError struct {
	StoreID RetentionStoreID `json:"store_id"`
	Message string           `json:"message"`
}

// ScanOptions configures discovery scope for scan providers.
type ScanOptions struct {
	Roots []string
	// Excludes holds user exclusion patterns (--exclude flags). Exclusions
	// only remove discovered paths from results; they never broaden deletion
	// authority.
	Excludes   []string
	OnProgress func(ScanProgressEvent)
}

// ExcludeSource identifies where an exclusion pattern came from.
type ExcludeSource string

const (
	ExcludeSourceFlag       ExcludeSource = "flag"
	ExcludeSourceIgnoreFile ExcludeSource = "ignore-file"
)

// ExcludedScope describes one honored user exclusion pattern. Exclusions
// affect discovery only and never broaden deletion authority.
type ExcludedScope struct {
	Pattern  string        `json:"pattern"`
	Resolved string        `json:"resolved"`
	Source   ExcludeSource `json:"source"`
	Count    int           `json:"count"`
}

// RejectedExclude describes an exclusion pattern that was not honored because
// it could not be scoped inside the approved scan roots.
type RejectedExclude struct {
	Pattern string        `json:"pattern"`
	Source  ExcludeSource `json:"source"`
	Reason  string        `json:"reason"`
}

// ScanProgressState describes the lifecycle point for a scan provider.
type ScanProgressState string

const (
	ScanProgressStart ScanProgressState = "start"
	ScanProgressDone  ScanProgressState = "done"
	ScanProgressError ScanProgressState = "error"
)

// ScanProgressEvent reports provider-level scan activity.
type ScanProgressEvent struct {
	State ScanProgressState
	Tool  Tool
	Count int
	Size  int64
	Err   error
}

// CategorySummary reports aggregate stats for a single category.
type CategorySummary struct {
	Count int
	Size  int64
}

// ToolSummary reports aggregate stats for a single tool.
type ToolSummary struct {
	Count int
	Size  int64
}

// PruneOptions configures the filtering and deletion behavior of a clean operation.
type PruneOptions struct {
	Age                    time.Duration
	Categories             []Category
	Tools                  []Tool
	DryRun                 bool
	Interactive            bool
	Risky                  bool
	Force                  bool
	IncludeActiveWorktrees bool
}
