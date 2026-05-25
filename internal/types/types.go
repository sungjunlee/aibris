package types

import "time"

type Tool string

const (
	ToolCodex    Tool = "codex"
	ToolClaude   Tool = "claude"
	ToolCursor       Tool = "cursor"
	ToolWindsurf     Tool = "windsurf"
	ToolNodeModules  Tool = "node_modules"
	ToolBuildCache   Tool = "build-cache"
	ToolPipCache     Tool = "pip-cache"
)

type Category string

const (
	CategoryWorktree    Category = "worktree"
	CategoryNodeModules Category = "node_modules"
	CategoryBuildCache  Category = "build-cache"
	CategoryOtherCache  Category = "other-cache"
)

type WorktreeInfo struct {
	Tool     Tool
	Category Category
	ID       string
	Project  string
	Path     string
	Size     int64
	ModTime  time.Time
}

type ScanResult struct {
	Worktrees  []WorktreeInfo
	TotalCount int
	TotalSize  int64
	ByCategory map[Category]CategorySummary
	ByTool     map[Tool]ToolSummary
}

type CategorySummary struct {
	Count int
	Size  int64
}

type ToolSummary struct {
	Count int
	Size  int64
}

type PruneOptions struct {
	Age         time.Duration
	Categories  []Category
	Tools       []Tool
	DryRun      bool
	Interactive bool
}
