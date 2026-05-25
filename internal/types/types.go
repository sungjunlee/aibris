package types

import "time"

type Tool string

const (
	ToolCodex    Tool = "codex"
	ToolClaude   Tool = "claude"
	ToolCursor   Tool = "cursor"
	ToolWindsurf Tool = "windsurf"
)

type WorktreeInfo struct {
	Tool       Tool
	ID         string
	Project    string
	Path       string
	Size       int64
	ModTime    time.Time
	LastCommit *time.Time
}

type ScanResult struct {
	Worktrees  []WorktreeInfo
	TotalSize  int64
	TotalCount int
}

type PruneOptions struct {
	Age         time.Duration
	Tools       []Tool
	All         bool
	DryRun      bool
	Interactive bool
	Force       bool
}
