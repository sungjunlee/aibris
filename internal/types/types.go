package types

import "time"

// Tool identifies the AI tool that created the debris.
type Tool string

const (
	ToolCodex       Tool = "codex"
	ToolClaude      Tool = "claude"
	ToolCursor      Tool = "cursor"
	ToolWindsurf    Tool = "windsurf"
	ToolNodeModules Tool = "node_modules"
	ToolBuildCache  Tool = "build-cache"
	ToolPipCache    Tool = "pip-cache"
	ToolAILogs      Tool = "ai-logs"
)

// Category classifies the type of debris.
type Category string

const (
	CategoryWorktree    Category = "worktree"
	CategoryNodeModules Category = "node_modules"
	CategoryBuildCache  Category = "build-cache"
	CategoryOtherCache  Category = "other-cache"
	CategoryAILogs      Category = "ai-logs"
)

// IsRisky reports whether this category requires explicit --risky opt-in.
// AI logs and unknown categories default to risky (safe-by-default).
func (c Category) IsRisky() bool {
	switch c {
	case CategoryWorktree, CategoryNodeModules, CategoryBuildCache, CategoryOtherCache:
		return false
	case "": // backward compat: pre-Category entries are safe
		return false
	default:
		return true
	}
}

// DebrisInfo describes a single debris item found during scanning.
type DebrisInfo struct {
	Tool     Tool
	Category Category
	ID       string
	Project  string
	Path     string
	Size     int64
	ModTime  time.Time
}

// ScanResult aggregates all debris found by all adapters.
type ScanResult struct {
	Worktrees  []DebrisInfo
	TotalCount int
	TotalSize  int64
	ByCategory map[Category]CategorySummary
	ByTool     map[Tool]ToolSummary
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
	Age         time.Duration
	Categories  []Category
	Tools       []Tool
	DryRun      bool
	Interactive bool
	Risky       bool
	Force       bool
}
